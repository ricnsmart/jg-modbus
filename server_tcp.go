package modbus

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
	"time"
)

type ErrorLevel int

const (
	ERROR ErrorLevel = iota + 1
	DEBUG
)

// Handler 京硅设备上报数据处理器
// addr 连接标识
// data 上报数据
// response 用于收集设备响应server下发的命令，payload是来自设备的应答
type Handler func(addr net.Addr, data []byte, answer func(payload []byte) error)

type Server struct {
	addr string

	handle Handler

	readTimeout time.Duration

	writeTimeout time.Duration

	// 存储连接
	// 用于主动关闭连接
	connMap sync.Map

	// 设备上报数据，server一次读取多少个字节
	readSize int

	onConnClose func(addr net.Addr)

	logLevel ErrorLevel
}

var DefaultReadTimeout = 90 * time.Second

var DefaultWriteTimeout = 90 * time.Second

var DefaultMaxReadSize = 512

func NewServer(addr string, handle Handler) *Server {
	return &Server{
		addr:         addr,
		handle:       handle,
		readTimeout:  DefaultReadTimeout,
		writeTimeout: DefaultWriteTimeout,
		readSize:     DefaultMaxReadSize,
	}
}

func (s *Server) SetReadTimeout(t time.Duration) {
	s.readTimeout = t
}

func (s *Server) SetWriteTimeout(t time.Duration) {
	s.writeTimeout = t
}

func (s *Server) SetMaxReadSize(size int) {
	s.readSize = size
}

func (s *Server) SetOnConnClose(f func(addr net.Addr)) {
	s.onConnClose = f
}

func (s *Server) SetLogLevel(logLevel ErrorLevel) {
	s.logLevel = logLevel
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}

	defer ln.Close()

	for {
		rwc, err := ln.Accept()
		if err != nil {
			return err
		}

		go s.newConn(rwc).serve()
	}
}

func (s *Server) CloseConn(addr any) error {
	v, ok := s.connMap.Load(addr)
	if !ok {
		return errors.New("connection not found")
	}
	return v.(*conn).rwc.Close()
}

func (s *Server) DownloadCommandToAll(ctx context.Context, cmd []byte, callback func(addr any, err error)) {

	s.connMap.Range(func(key, value any) bool {
		conn := value.(*conn)

		conn.mu.Lock()
		defer conn.mu.Unlock()

		select {
		case <-conn.ctx.Done():
			callback(key, errors.New("connection closed"))
			return false
		case <-ctx.Done():
			callback(key, errors.New("download closed"))
			return false
		case conn.downCh <- cmd:

		}

		select {
		case <-conn.ctx.Done():
			callback(key, errors.New("connection closed"))
		case <-ctx.Done():
			callback(key, errors.New("response timeout"))
		case <-conn.respCh:

		}
		return false
	})

}

func (s *Server) DownloadCommand(ctx context.Context, addr any, cmd []byte) ([]byte, error) {
	c, ok := s.connMap.Load(addr)
	if !ok {
		return nil, errors.New("connection not found")
	}

	conn := c.(*conn)

	conn.mu.Lock()
	defer conn.mu.Unlock()

	select {
	case <-conn.ctx.Done():
		return nil, errors.New("connection closed")
	case <-ctx.Done():
		return nil, errors.New("download timeout")
	case conn.downCh <- cmd:

	}

	select {
	case <-conn.ctx.Done():
		return nil, errors.New("connection closed")
	case <-ctx.Done():
		return nil, errors.New("response timeout")
	case resp := <-conn.respCh:
		return resp, nil
	}
}

func (s *Server) newConn(rwc net.Conn) *conn {

	ctx, cancel := context.WithCancel(context.Background())

	c := &conn{
		server: s,
		rwc:    rwc,
		downCh: make(chan []byte),
		respCh: make(chan []byte),
		ctx:    ctx,
		cancel: cancel,
	}

	s.connMap.Store(rwc.RemoteAddr(), c)

	return c
}

type conn struct {
	// server is the server on which the connection arrived.
	// Immutable; never nil.
	server *Server

	// rwc is the underlying network connection.
	// This is never wrapped by other types and is the value given out
	// to CloseNotifier callers. It is usually of type *net.TCPConn or
	// *tls.conn.
	rwc net.Conn

	// downCh 用户向设备下发的数据
	downCh chan []byte

	// respCh 用于存放下发后设备的响应
	respCh chan []byte

	// 控制连接的关闭
	ctx context.Context

	cancel context.CancelFunc

	// 控制写入频率
	mu sync.Mutex
}

func (c *conn) serve() {

	// upCh 设备上发的数据
	upCh := make(chan []byte)
	defer close(upCh)

	// 设备离线
	// 同时这也是判断设备是否在线的依据
	defer func() {
		c.server.onConnClose(c.rwc.RemoteAddr())
		c.server.connMap.Delete(c.rwc)
		c.rwc.Close()
	}()

	go func() {
		for {
			_ = c.rwc.SetReadDeadline(time.Now().Add(c.server.readTimeout))

			buf := make([]byte, c.server.readSize)

			l, err := c.rwc.Read(buf)
			if err != nil {
				if c.server.logLevel >= ERROR {
					log.Printf("ERROR %v\n", err)
				}
				c.cancel()
				return
			}

			buf = buf[:l]

			if c.server.logLevel == DEBUG {
				log.Printf("DEBUG Read: % x\n", buf)
			}

			upCh <- buf
			_ = c.rwc.SetReadDeadline(time.Time{})
		}
	}()

	for {
		select {
		case <-c.ctx.Done():
			return
		case packet := <-upCh:
			go c.server.handle(c.rwc.RemoteAddr(), packet, func(payload []byte) error {
				if err := c.write(payload); err != nil {
					c.cancel()
					return err
				}
				return nil
			})
		case payload := <-c.downCh:
			if err := c.write(payload); err != nil {
				c.cancel()
				return
			}
			ticker := time.NewTicker(c.server.readTimeout)

			select {
			case <-ticker.C:
			case data := <-upCh:
				// 可能错误的接收心跳包，需校验
				c.respCh <- data
			}
			ticker.Stop()
		}
	}
}

func (c *conn) write(buf []byte) error {
	_ = c.rwc.SetWriteDeadline(time.Now().Add(c.server.writeTimeout))

	defer c.rwc.SetReadDeadline(time.Time{})

	if c.server.logLevel == DEBUG {
		log.Printf("DEBUG write: % x\n", buf)
	}

	if _, err := c.rwc.Write(buf); err != nil {
		return err
	}

	return nil
}
