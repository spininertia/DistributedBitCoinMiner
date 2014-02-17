// Contains the implementation of a LSP client.

package lsp

import (
	"container/list"
	"encoding/json"
	"errors"
	"github.com/cmu440/lspnet"
	"log"
	"os"
	"time"
)

const (
	Bufsize = 1500
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
	connAckChan       chan struct{}        // notify NewClient conn ack is received
	readReqChan       <-chan *readRequest  // communicate with Read
	closeReqChan      <-chan *closeRequest // communicate with Close
	writeReqChan      <-chan []byte        //communicate with Write
	msgArriveChan     <-chan *Message      //new msg arrived
	epochChan         <-chan struct{}      // receive epoch notification
	shutdownNtwkChan  chan struct{}        // to shutdown ntwk handler
	shutdownEpochChan chan struct{}        // to shutdown epoch handler
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
	c := &client{
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
		writeReqChan:      make(chan []byte),
		msgArriveChan:     make(chan *Message),
		epochChan:         make(chan struct{}),
		shutdownNtwkChan:  make(chan struct{}),
		shutdownEpochChan: make(chan struct{}),
	}

	// dial UDP
	udpAddr := lspnet.ResolveUDPAddr("udp", hostport)
	c.conn = lspnet.DialUDP("udp", nil, udpAddr)

	// start several goroutines
	go c.masterEventHandler()
	go c.networkEventHandler()
	go c.epochHandler()

	// send conn request to server
	c.sendMsg(NewConnect())

	// waiting for conn established signal
	_, err := <-c.connAckChan
	if err != nil {
		// failed
		LOGV.Println("Connect Failed")
		return nll, err
	}

	return c, nil
}

func (c *client) ConnID() int {
	return client.connId
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

/*
 * helper functions
 */
func (c *client) sendMsg(msg *Message) {
	packet = json.Marshal(msg)
	lspnet.Write(packet)

	LOGV.Printf("message sent: %s ", msg.String())
}

/*
 * handlers/goroutines
 */

// master goroutine, control access to share resources
func (c *client) masterEventHandler() {
	for {
		select {
		case req := <-c.readReqChan:
			// TODO: handle read request
		case payload := <-c.writeReqChan:
			// TODO: handle write request
		case req := <-c.closeReqChan:
			// TODO: handle close request
		case msg := <-c.msgArriveChan:
			// TODO: handle msg received from ntwk handler
		case <-c.epochChan:
			// TODO handle timeout, lost, resend...
		}
	}
}

// network event handler, listen on port for msgs
func (c *client) networkEventHandler() {
	LOGV.Println("Network Event Handler started!")

	buffer := make([]byte, 1500)
	msg := new(Message)

	for {
		select {
		case <-c.shutdownNtwkChan:
			LOGV.Println("Network EventHandler shutdown!")
			return
		default:
			lspnet.Read(buffer)
			json.Unmarshal(buffer, msg)
			c.msgArriveChan <- msg
		}
	}
}

// handles epoch events
func (c *client) epochHandler() {
	LOGV.Println("Epoch Handler started!")

	for {
		select {
		case <-time.After(DefaultEpochMillis * time.Millisecond):
			c.epochChan <- struct{}{}
		case <-c.shutdownEpochChan:
			LOGV.Println("Epoch Handler shutdown!")
			return
		}
	}
}
