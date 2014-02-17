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

var LOGE = log.New(os.Stderr, "ERROR", log.Lmicroseconds|log.Lshortfile)
var LOGV = log.New(ioutil.Discard, "VERBOSE ", log.Lmicroseconds|log.Lshortfile)

type client struct {
	connId int             // connection id
	conn   *lspnet.UDPConn // udp connection

	// status fields
	//	isConnected bool // duplicated, can be deduceted by expectedSeqId
	isClosed bool // indicates whether application has called close method
	isLost   bool // indicates whether the connection is lost

	// sequence ids
	expectedSeqId    int // expected server side data msg seq id
	clientSeqId      int // next seq id of client data msg
	maxReceivedSeqId int // maximum received data msg sequence id

	// timeout counters
	noMsgEpochCount  int  // #consecutive epochs that no msg is received
	recvMsgLastEpoch bool // indicates whether msg is received in last epoch

	// bufs
	sentMsgBuf     *list.List       // msgs wating to be acked or sent
	receivedMsgBuf map[int]*Message //received msgs buf, key is sequence id

	// request queues
	readRequestQueue  *list.List // read requests waiting to be responded
	closeRequestQueue *list.List // close requests waiting to be responded

	// channels
	connAckChan        chan struct{}      // notify NewClient conn ack is received
	readReqChan        chan *readRequest  // communicate with Read
	closeReqChan       chan *closeRequest // communicate with Close
	writeReqChan       chan *writeRequest //communicate with Write
	msgArriveChan      chan *Message      //new msg arrived
	epochChan          chan struct{}      // receive epoch notification
	shutdownNtwkChan   chan struct{}      // to shutdown ntwk handler
	shutdownEpochChan  chan struct{}      // to shutdown epoch handler
	shutdownMasterChan chan struct{}      //to shutdown master handler

	// params
	params *Params
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
		isClosed:           false,
		isLost:             false,
		expectedSeqId:      0,
		clientSeqId:        0,
		maxReceivedSeqId:   -1,
		noMsgEpochCount:    0,
		recvMsgLastEpoch:   false,
		sentMsgBuf:         list.New(),
		receivedMsgBuf:     make(map[int]*Message),
		readRequestQueue:   list.New(),
		closeRequestQueue:  list.New(),
		connAckChan:        make(chan struct{}),
		readReqChan:        make(chan *readRequest),
		closeReqChan:       make(chan *closeRequest),
		writeReqChan:       make(chan *writeRequest),
		msgArriveChan:      make(chan *Message),
		epochChan:          make(chan struct{}),
		shutdownNtwkChan:   make(chan struct{}),
		shutdownEpochChan:  make(chan struct{}),
		shutdownMasterChan: make(chan struct{}),
		params:             params,
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
	LOGV.Println("Connection Established")

	return c, nil
}

func (c *client) ConnID() int {
	return client.connId
}

func (c *client) Read() ([]byte, error) {
	request := newReadRequest()
	payload, err := <-request.response
	if err != nil {
		return err
	}
	return payload, nil
}

func (c *client) Write(payload []byte) error {
	request := newWriteRequest(payload)
	_, err := <-writeRequest.response
	if err != nil {
		return errors.New("Connection has been lost!")
	}
	return nil
}

func (c *client) Close() error {
	LOGV.Println("Close requested!")
	request := newCloseRequest()
	c.closeReqChan <- request
	<-request.response
	LOGV.Println("Close responded! Client shutdown!")
	return nil
}

/*
 * helper functions
 */

// marshalling and send message via udp connection
func (c *client) sendMsg(msg *Message) {
	packet := json.Marshal(msg)
	lspnet.Write(packet)

	LOGV.Printf("message sent: %s ", msg.String())
}

//
func (c *client) getMinExpectedSeqId() int {
	if c.readRequestQueue.Len() > 0 {
		return c.readRequestQueue.Front().Value.(readRequest).expectedSeqId
	}
	return c.expectedSeqId
}

func (c *client) handleReadRequest(request *readRequest) {
	// first check whether the connection is closed or lost
	if c.isLost || c.isClosed {
		close(request.response)
	} else {
		// TODO recheck
		readRequest.expectedSeqId = c.expectedSeqId
		c.expectedSeqId++
		// TODO:check whether requested msg is in buf
		// if yes, grab and go, also remember to update sequence ids..
		// possibly delete some entries in bufmap
	}
}

func (c *client) handleWriteRequest(request *writeRequest) {
	// first check if the connection has been lost
	if c.isLost {
		close(request.response)
	} else {
		// notify Write that request accpepted
		request.response <- struct{}{}

		// send data message
		dataMsg := NewData(c.connId, c.clientSeqId, request.payload)
		c.clientSeqId++
		c.sendMsg(dataMsg)

		// add data message to sentMsgBuf
		c.sentMsgBuf.PushBack(dataMsg)
	}
}

func (c *client) handleCloseRequest(request *closeRequest) {
	// first check if connnection is lost or sentBuf is empty
	if c.isLost || c.sentMsgBuf.Len() == 0 {
		//if not lost, still needs to close ntwk and epoch handler
		if !c.isLost {
			close(c.shutdownEpochChan)
			close(c.shutdownNtwkChan)
		}
		close(c.shutdownMasterChan)
		c.isClosed = true
		request.response <- struct{}{}
	} else {
		// push into queue, waiting to be processed
		c.closeRequestQueue.PushBack(request)
	}
}

// handle newly arrived messages
func (c *client) handleNewMsg(msg *Message) {
	if msg.Type == MsgAck {
		if c.expectedSeqId == 0 {
			// update status
			c.expectedSeqId = 1
			c.maxReceivedSeqId = 0
			c.clientSeqId = 1

			// notify NewClient
			c.connAckChan <- struct{}{}
		} else {
			// check against sentMsgBuf
			for e := c.sentMsgBuf.Front(); e != nil; e = e.Next() {
				if msg.SeqNum == e.Value.(*Message).SeqNum {
					c.sentMsgBuf.Remove(e)
					break
				}
			}

			// check whether there is pending close request
			if c.closeRequestQueue.Len() > 0 && c.sentMsgBuf.Len() == 0 {
				c.notifyClose()
			}
		}
	} else {
		// TODO: handle data messages

	}
}

// handle epoch event, do regular checkup
func (c *client) handleEpochEvent() {
	// check timeout
	if !c.recvMsgLastEpoch {
		c.noMsgEpochCount++
		LOGV.Println("It's been %d epoch that no msg was received",
			c.noMsgEpochCount)
	} else {
		c.noMsgEpochCount = 0
	}
	c.recvMsgLastEpoch = false

	// lost
	if c.noMsgEpochCount >= c.params.EpochLimit {
		c.isLost = true
		close(c.shutdownEpochChan) // stop epoch event handler
		close(c.shutdownNtwkChan)  // stop reading from ntwk
		c.epochChan = nil
		// TODO: tricky here, check again!
		if c.closeRequestQueue.Len() > 0 {
			c.notifyClose()
		}
		return
	}

	// if conn msg hasn't been ACKed
	if c.expectedSeqId == 0 {
		c.sendMsg(NewConnect())
	}

	// if conn established but no msg received
	if c.getMinExpectedSeqId() == 1 && len(c.receivedMsgBuf) == 0 {
		c.sendMsg(NewAck(c.connId, 0))
	}

	// resend messages that has not been ACKed
	// apply sliding window here
	if c.sentMsgBuf.Len() > 0 {
		windowUpperLimit := c.sentMsgBuf.Front().Value.(*Message).SeqNum +
			c.params.WindowSize
		for e := c.sentMsgBuf.Front(); e != nil &&
			e.Value.(*Message).SeqNum < windowTestMode; e = e.Next() {
			c.sendMsg(e.Value.(*Message))
		}
	}

	// resend ACKs
	if c.maxReceivedSeqId > 0 {
		for seqId := c.maxReceivedSeqId; seqId > 0 &&
			seqId > c.maxReceivedSeqId-c.params.WindowSize; seqId-- {
			if msg, ok := c.receivedMsgBuf[seqId]; ok {
				c.sendMsg(NewAck(c.connId, msg.SeqNum))
			}
		}
	}

}

/*
 * notifers, notify blocked pending request
 */

// handle pending Close request
func (c *client) notifyClose() {
	// may be called when close is not imediately responded
	// 1. timeout and needs close
	// 2. needs close and the last unacked/unsent msg was acked
	close(c.shutdownMasterChan)
	c.isClosed = true
	req := c.closeRequestQueue.Remove(c.closeRequestQueue.Front())
	req.response <- struct{}{}
}

// handle pending Read requests
func (c *client) notifyRead() {
	// tricky here, be careful
}

/*
 * handlers/goroutines
 */

// master goroutine, control access to share resources
func (c *client) masterEventHandler() {
	defer c.conn.Close()
	for {
		select {
		case request := <-c.readReqChan:
			c.handleReadRequest(request)
		case request := <-c.writeReqChan:
			c.handleWriteRequest(request)
		case request := <-c.closeReqChan:
			c.handleCloseRequest(request)
		case msg := <-c.msgArriveChan:
			c.handleNewMsg(msg)
		case <-c.epochChan:
			c.handleEpochEvent()
		case <-c.shutdownMasterChan:
			LOGV.Println("Master Event Handler shutdown!")
			return
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
		case <-time.After(c.params.EpochMillis * time.Millisecond):
			c.epochChan <- struct{}{}
		case <-c.shutdownEpochChan:
			LOGV.Println("Epoch Handler shutdown!")
			return
		}
	}
}
