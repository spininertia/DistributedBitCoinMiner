// define read, write and close request types

package lsp

import (
	"github.com/cmu440/lspnet"
)

type readRequest struct {
	expectedSeqId int
	response      chan []byte
}

func newReadRequest() *readRequest {
	return &readRequest{
		response: make(chan []byte),
	}
}

type writeRequest struct {
	payload  []byte
	response chan struct{}
}

func newWriteRequest(payload []byte) *writeRequest {
	return &writeRequest{
		payload:  payload,
		response: make(chan struct{}),
	}
}

type closeRequest struct {
	response chan struct{}
}

func newCloseRequest() *closeRequest {
	return &closeRequest{
		response: make(chan struct{}),
	}
}

type serverWriteRequest struct {
	connId   int
	payload  []byte
	response chan struct{}
}

func newServerWriteRequest(connId int, payload []byte) *serverWriteRequest {
	return &serverWriteRequest{
		connId:   connId,
		payload:  payload,
		response: make(chan struct{}),
	}
}

type closeConnRequest struct {
	connId   int
	response chan struct{}
}

func newCloseConnRequest(connId int) *closeConnRequest {
	return &closeConnRequest{
		connId:   connId,
		response: make(chan struct{}),
	}
}

// type connectRequest struct {
// 	conn *lspnet.UDPConn
// }

// func newConnectRequest(conn *lspnet.UDPConn) *connectRequest {
// 	return &connectRequest{
// 		conn: conn,
// 	}
// }

type serverReadResponse struct {
	connId    int
	payload   []byte
	isSuccess bool
}

func newServerReadResponse(connId int, payload []byte,
	isSuccess bool) *serverReadResponse {
	return &serverReadResponse{
		connId:    connId,
		payload:   payload,
		isSuccess: isSuccess,
	}
}

type serverReadRequest struct {
	response chan *serverReadResponse
}

func newServerReadRequest() *serverReadRequest {
	return &serverReadRequest{
		response: make(chan *serverReadResponse),
	}
}

type packet struct {
	msg  *Message
	addr *lspnet.UDPAddr
}

func newPacket(msg *Message, addr *lspnet.UDPAddr) *packet {
	return &packet{
		msg:  msg,
		addr: addr,
	}
}
