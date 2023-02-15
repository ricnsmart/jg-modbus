package modbus

import (
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

	readTimeout time.Duration

	writeTimeout time.Duration

	// 设备上报数据，server一次读取多少个字节
	readSize int

	serve func(conn *Conn)

	logLevel ErrorLevel
}

var DefaultReadTimeout = 90 * time.Second

var DefaultWriteTimeout = 90 * time.Second

var DefaultMaxReadSize = 512

func NewServer(addr string) *Server {
	return &Server{
		addr:         addr,
		readTimeout:  DefaultReadTimeout,
		writeTimeout: DefaultWriteTimeout,
		readSize:     DefaultMaxReadSize,
	}
}

func (s *Server) SetServe(serve func(conn *Conn)) {
	s.serve = serve
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

		go s.serve(&Conn{rwc: rwc, server: s})
	}
}

type Conn struct {
	// server is the server on which the connection arrived.
	// Immutable; never nil.
	server *Server

	// rwc is the underlying network connection.
	// This is never wrapped by other types and is the value given out
	// to CloseNotifier callers. It is usually of type *net.TCPConn or
	// *tls.conn.
	rwc net.Conn

	mu sync.Mutex
}

func (c *Conn) Read() ([]byte, error) {
	_ = c.rwc.SetReadDeadline(time.Now().Add(c.server.readTimeout))

	defer c.rwc.SetReadDeadline(time.Time{})

	buf := make([]byte, c.server.readSize)

	l, err := c.rwc.Read(buf)
	if err != nil {
		if c.server.logLevel >= ERROR {
			log.Printf("ERROR %v Read %v\n", c.rwc.RemoteAddr(), err)
		}
		return nil, err
	}

	if c.server.logLevel == DEBUG {
		log.Printf("DEBUG %v Read: % x\n", c.rwc.RemoteAddr(), buf[:l])
	}

	return buf[:l], nil
}

func (c *Conn) Write(buf []byte) error {
	_ = c.rwc.SetWriteDeadline(time.Now().Add(c.server.writeTimeout))

	defer c.rwc.SetWriteDeadline(time.Time{})

	if c.server.logLevel == DEBUG {
		log.Printf("DEBUG %v write: % x\n", c.rwc.RemoteAddr(), buf)
	}

	_, err := c.rwc.Write(buf)

	return err
}

func (c *Conn) Close() error {
	return c.rwc.Close()
}

func (c *Conn) Addr() net.Addr {
	return c.rwc.RemoteAddr()
}

func (c *Conn) Lock() {
	c.mu.Lock()
}

func (c *Conn) Unlock() {
	c.mu.Unlock()
}

func (c *Conn) NewRequest(frame *Frame) (*Frame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.Write(frame.Bytes()); err != nil {
		return nil, err
	}

	buf, err := c.Read()
	if err != nil {
		return nil, err
	}
	respFrame, err := NewFrame(buf)
	if err != nil {
		return nil, err
	}
	return respFrame, nil
}
