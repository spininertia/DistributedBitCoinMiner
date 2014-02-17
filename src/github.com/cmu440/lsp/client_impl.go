// Contains the implementation of a LSP client.

package lsp

import (
	"container/list"
	"errors"
	"github.com/cmu440/lspnet"
	"log"
	"os"
)

var LOGE = log.new(os.Stderr, "ERROR", log.Lmicroseconds|log.Lshortfile)
var LOGV = log.New(ioutil.Discard, "VERBOSE ", log.Lmicroseconds|log.Lshortfile)

type client struct {
	connId int             // connection id
	conn   *lspnet.UDPConn // udp connection

	// status fields
	isConnected bool // indicates whether conn request msg is acked
	isClosed    bool // indicates whether application has called close method
	isLost      bool // indicates whether the connection is lost

	// sequence ids
	expectedSeqId    int // expected server side data msg seq id
	clientSeqId      int // next seq id of client data msg
	minAckedSeqId    int // smallest seq id that is Acked
	maxReceivedSeqId int // maximum received data msg sequence id

	// timeout counters
	noMsgEpochCount  int  // #consecutive epochs that no msg is received
	recvMsgLastEpoch bool // indicates whether msg is received in last epoch

	// bufs
	sentMsgBufs    *list.List       // msgs wating to be acked or sent
	receivedMsgBuf map[int]*Message //received msgs buf, key is sequence id

	// request queues
	readRequestQueue  *list.List // read requests waiting to be responded
	closeRequestQueue *list.List // close requests waiting to be responded

	// channels
	connAckChan  chan struct{}        // notify NewClient conn ack is received
	readReqChan  <-chan *readRequest  // communicate with Read
	closeReqChan <-chan *closeRequest // communicate with Close
	shutdown     chan struct{}        // to shutdown all goroutines
}

// NewClient creates, initiates, and returns a new client. This function
// should return after a connection with the server has been established
// (i.e., the client has received an Ack message from the server in response
// to its connection request), and should return a non-nil error if a
// connection could not be made (i.e., if after K epochs, the client still
// hasn't received an Ack message from the server in response to its K
// connection requests).
//
// hostport is a colon-separated string identifying the server's host address
// and port number (i.e., "localhost:9999").
func NewClient(hostport string, params *Params) (Client, error) {

	// initialize client struct
	cl := &client{
		isConnected:       false,
		isClosed:          false,
		isLost:            false,
		expectedSeqId:     0,
		clientSeqId:       0,
		minAckedSeqid:     -1,
		maxReceivedSeqId:  -1,
		noMsgEpochCount:   0,
		recvMsgLastEpoch:  false,
		sentMsgBufs:       list.New(),
		receivedMsgBuf:    make(map[int]*Message),
		readRequestQueue:  list.New(),
		closeRequestQueue: list.New(),
		connAckChan:       make(chan struct{}),
		readReqChan:       make(<-chan *readRequest),
		closeReqChan:      make(<-chan *closeRequest),
		shutdown:          make(chan struct{}),
	}

	// dial UDP
	udpAddr := lspnet.ResolveUDPAddr("udp", hostport)

	return nil, errors.New("not yet implemented")
}

func (c *client) ConnID() int {
	return -1
}

func (c *client) Read() ([]byte, error) {
	// TODO: remove this line when you are ready to begin implementing this method.
	select {} // Blocks indefinitely.
	return nil, errors.New("not yet implemented")
}

func (c *client) Write(payload []byte) error {
	return errors.New("not yet implemented")
}

func (c *client) Close() error {
	return errors.New("not yet implemented")
}
