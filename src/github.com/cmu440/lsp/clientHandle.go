// contains server side client handle

package lsp

import (
	"github.com/cmu440/lspnet"
)

type clientHandle struct {
	connId  int             // connection id
	udpAddr *lspnet.UDPAddr // remote client udp addr
	// status fields
	isClosed bool // indicates whether application has called close method
	isLost   bool // indicates whether the connection is lost

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
