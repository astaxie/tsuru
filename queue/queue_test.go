// Copyright 2012 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package queue

import (
	"bytes"
	"encoding/gob"
	. "launchpad.net/gocheck"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func Test(t *testing.T) {
	TestingT(t)
}

type S struct{}

var _ = Suite(&S{})

// SafeBuffer is a thread safe buffer.
type SafeBuffer struct {
	closed int32
	buf    bytes.Buffer
	sync.Mutex
}

func (sb *SafeBuffer) Read(p []byte) (int, error) {
	sb.Lock()
	defer sb.Unlock()
	return sb.buf.Read(p)
}

func (sb *SafeBuffer) Write(p []byte) (int, error) {
	sb.Lock()
	defer sb.Unlock()
	return sb.buf.Write(p)
}

func (sb *SafeBuffer) Close() error {
	atomic.StoreInt32(&sb.closed, 1)
	return nil
}

func (s *S) TestChannelFromWriter(c *C) {
	var buf SafeBuffer
	message := Message{
		Action: "delete",
		Args:   []string{"everything"},
	}
	ch, _ := ChannelFromWriter(&buf)
	defer close(ch)
	ch <- message
	time.Sleep(1e6)
	var decodedMessage Message
	decoder := gob.NewDecoder(&buf)
	err := decoder.Decode(&decodedMessage)
	c.Assert(err, IsNil)
	c.Assert(decodedMessage, DeepEquals, message)
}

func (s *S) TestClosesErrChanWhenClientCloseMessageChannel(c *C) {
	var buf SafeBuffer
	ch, errCh := ChannelFromWriter(&buf)
	close(ch)
	_, ok := <-errCh
	c.Assert(ok, Equals, false)
}

func (s *S) TestClosesWriteCloserWhenClientClosesMessageChannel(c *C) {
	var buf SafeBuffer
	ch, _ := ChannelFromWriter(&buf)
	close(ch)
	time.Sleep(1e6)
	c.Assert(atomic.LoadInt32(&buf.closed), Equals, int32(1))
}

func (s *S) TestWriteSendErrorsInTheErrorChannel(c *C) {
	messages := make(chan Message, 1)
	errCh := make(chan error, 1)
	conn := NewFakeConn("127.0.0.1:2345", "127.0.0.1:12345")
	conn.Close()
	go write(conn, messages, errCh)
	messages <- Message{}
	close(messages)
	err, ok := <-errCh
	c.Assert(ok, Equals, true)
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, "Closed connection.")
}

func (s *S) TestHandleSendErrorsInTheErrorsChannel(c *C) {
	conn := NewFakeConn("127.0.0.1:8000", "127.0.0.1:4000")
	server := Server{
		pairs: make(chan pair, 1),
	}
	conn.Close()
	go server.handle(conn)
	pair := <-server.pairs
	c.Assert(pair.err, NotNil)
	c.Assert(pair.err.Error(), Equals, "Closed connection.")
}

func (s *S) TestServerAddr(c *C) {
	listener := NewFakeListener("0.0.0.0:8000")
	server := Server{listener: listener}
	c.Assert(server.Addr(), Equals, listener.Addr().String())
}

func (s *S) TestServerDoubleClose(c *C) {
	server, err := StartServer("127.0.0.1:0")
	c.Assert(err, IsNil)
	err = server.Close()
	c.Assert(err, IsNil)
	err = server.Close()
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, "Server already closed.")
}

func (s *S) TestStartServerAndReadMessage(c *C) {
	message := Message{
		Action: "delete",
		Args:   []string{"something"},
	}
	server, err := StartServer("127.0.0.1:0")
	c.Assert(err, IsNil)
	defer server.Close()
	conn, err := net.Dial("tcp", server.Addr())
	c.Assert(err, IsNil)
	defer conn.Close()
	encoder := gob.NewEncoder(conn)
	err = encoder.Encode(message)
	c.Assert(err, IsNil)
	gotMessage, err := server.Message(2e9)
	c.Assert(err, IsNil)
	c.Assert(gotMessage, DeepEquals, message)
}

func (s *S) TestMessageNegativeTimeout(c *C) {
	server := Server{
		pairs: make(chan pair, 1),
	}
	defer close(server.pairs)
	var (
		got, want Message
		err       error
	)
	want = Message{Action: "create"}
	server.pairs <- pair{message: want}
	got, err = server.Message(-1)
	c.Assert(err, IsNil)
	c.Assert(got, DeepEquals, want)
}

func (s *S) TestPutBack(c *C) {
	server := Server{
		pairs: make(chan pair, 1),
	}
	want := Message{Action: "delete"}
	server.PutBack(want)
	got, err := server.Message(1e6)
	c.Assert(err, IsNil)
	want.Visits++
	c.Assert(got, DeepEquals, want)
}

func (s *S) TestDontHangWhenClientClosesTheConnection(c *C) {
	server, err := StartServer("127.0.0.1:0")
	c.Assert(err, IsNil)
	defer server.Close()
	messages, _, err := Dial(server.Addr())
	c.Assert(err, IsNil)
	close(messages)
	msg, err := server.Message(1e9)
	c.Assert(msg, DeepEquals, Message{})
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, "EOF: client disconnected.")
}

func (s *S) TestDontHangWhenServerClosesTheConnection(c *C) {
	server, err := StartServer("127.0.0.1:0")
	c.Assert(err, IsNil)
	for i := 0; i < 5; i++ {
		Dial(server.Addr())
	}
	time.Sleep(1e9)
	err = server.Close()
	c.Assert(err, IsNil)
}

func (s *S) TestDial(c *C) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, IsNil)
	defer listener.Close()
	received := make(chan Message, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			decoder := gob.NewDecoder(conn)
			var message Message
			if err = decoder.Decode(&message); err != nil {
				panic(err)
			}
			received <- message
		}
	}()
	sent := Message{
		Action: "delete",
		Args:   []string{"everything"},
	}
	messages, _, err := Dial(listener.Addr().String())
	c.Assert(err, IsNil)
	messages <- sent
	got := <-received
	c.Assert(got, DeepEquals, sent)
}

func (s *S) TestClientAndServerMultipleMessages(c *C) {
	server, err := StartServer("127.0.0.1:0")
	c.Assert(err, IsNil)
	defer server.Close()
	messages, errors, err := Dial(server.Addr())
	c.Assert(err, IsNil)
	go func() {
		for err := range errors {
			c.Fatal(err)
		}
	}()
	messageSlice := make([]Message, 10)
	for i := 0; i < 10; i++ {
		messageSlice[i] = Message{Action: "test", Args: []string{strconv.Itoa(i)}}
		messages <- messageSlice[i]
	}
	for i := 0; i < 10; i++ {
		if message, err := server.Message(-1); err == nil {
			c.Assert(message, DeepEquals, messageSlice[i])
		} else {
			c.Fatal(err)
		}
	}
}

// N clients, each sending 500 messages, concurrently.
func BenchmarkMultipleClients(b *testing.B) {
	server, err := StartServer("127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	addr := server.Addr()
	defer server.Close()
	go func() {
		for {
			if _, err := server.Message(-1); err != nil && err.Error() != "EOF: client disconnected." {
				return
			}
		}
	}()
	for i := 0; i < b.N; i++ {
		messages, errors, err := Dial(addr)
		if err != nil {
			b.Fatal(err)
		}
		go func(ch <-chan error) {
			for err := range ch {
				println(err.Error())
			}
		}(errors)
		go func(ch chan<- Message, n int) {
			for j := 0; j < 500; j++ {
				messages <- Message{
					Action: "handle-client",
					Args:   []string{strconv.Itoa(n + j)},
				}
			}
			close(ch)
		}(messages, i)
	}
}
