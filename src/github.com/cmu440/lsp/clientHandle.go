// contains server side client handle

package lsp

import (
	"container/list"
	"github.com/cmu440/lspnet"
)

type clientHandle struct {
	connId int             // connection id
	addr   *lspnet.UDPAddr // remote udp addr

	// status fields
	isClosed   bool // indicates whether application has called closeconn method
	isLost     bool // indicates whether the connection is lost
	isNotified bool // indicates whether a Server Read hass been notified err

	// sequence ids
	expectedSeqId    int // expected server side data msg seq id
	nextSendSeqId    int // next seq id of client data msg
	maxReceivedSeqId int // maximum received data msg sequence id

	// timeout counters
	noMsgEpochCount  int  // #consecutive epochs that no msg is received
	recvMsgLastEpoch bool // indicates whether msg is received in last epoch

	// bufs
	sentMsgBuf     *list.List       // msgs wating to be acked or sent
	receivedMsgBuf map[int]*Message // received msgs buf, key is sequence id
}

func newClientHandle(connId int, addr *lspnet.UDPAddr) *clientHandle {
	return &clientHandle{
		connId:           connId,
		addr:             addr,
		isClosed:         false,
		isLost:           false,
		isNotified:       false,
		expectedSeqId:    1,
		nextSendSeqId:    1,
		maxReceivedSeqId: 0,
		noMsgEpochCount:  0,
		recvMsgLastEpoch: false,
		sentMsgBuf:       list.New(),
		receivedMsgBuf:   make(map[int]*Message),
	}
}
