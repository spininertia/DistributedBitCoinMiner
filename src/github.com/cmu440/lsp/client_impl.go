// Contains the implementation of a LSP client.

package lsp

import (
	"container/list"
	"encoding/json"
	"errors"
	"github.com/cmu440/lspnet"
	"time"
)

type client struct {
	connId int             // connection id
	conn   *lspnet.UDPConn // udp connection

	// status fields
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
	receivedMsgBuf map[int]*Message // received msgs buf, key is sequence id

	// request queues
	readRequestQueue  *list.List // read requests waiting to be responded
	closeRequestQueue *list.List // close requests waiting to be responded

	// channels
	connAckChan        chan struct{}      // notify NewClient conn ack received
	readReqChan        chan *readRequest  // communicate with Read
	closeReqChan       chan *closeRequest // communicate with Close
	writeReqChan       chan *writeRequest // communicate with Write
	msgArriveChan      chan *Message      // new msg arrived
	epochChan          chan struct{}      // receive epoch notification
	shutdownNtwkChan   chan struct{}      // to shutdown ntwk handler
	shutdownEpochChan  chan struct{}      // to shutdown epoch handler
	shutdownMasterChan chan struct{}      // to shutdown master handler

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
	udpAddr, _ := lspnet.ResolveUDPAddr("udp", hostport)
	c.conn, _ = lspnet.DialUDP("udp", nil, udpAddr)
	// start several goroutines
	go c.masterEventHandler()
	go c.networkEventHandler()
	go c.epochHandler()

	// send conn request to server
	c.sendMsg(NewConnect())

	// waiting for conn established signal
	_, ok := <-c.connAckChan
	if !ok {
		// failed
		LOGV.Println("Connect Failed")
		return nil, errors.New("Connect Failed")
	}
	LOGV.Println("Connection Established")

	return c, nil
}

func (c *client) ConnID() int {
	return c.connId
}

func (c *client) Read() ([]byte, error) {
	request := newReadRequest()
	c.readReqChan <- request
	payload, ok := <-request.response
	if !ok {
		return nil, errors.New("Application Read Failed due to lost or close")
	}
	return payload, nil
}

func (c *client) Write(payload []byte) error {
	request := newWriteRequest(payload)
	c.writeReqChan <- request
	_, ok := <-request.response
	if !ok {
		return errors.New("Connection has been lost!")
	}
	return nil
}

func (c *client) Close() error {
	request := newCloseRequest()
	c.closeReqChan <- request
	<-request.response
	LOGV.Println("Close responded! Client shutdown!")
	return nil
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

	var buffer [Bufsize]byte

	for {
		select {
		case <-c.shutdownNtwkChan:
			LOGV.Println("Network EventHandler shutdown!")
			return
		default:
			msg := new(Message)
			n, err := c.conn.Read(buffer[0:])
			if err != nil {
				LOGE.Println(err)
			}
			json.Unmarshal(buffer[0:n], msg)
			c.msgArriveChan <- msg
		}
	}
}

// handles epoch events
func (c *client) epochHandler() {
	LOGV.Println("Epoch Handler started!")

	for {
		select {
		case <-time.After(time.Duration(c.params.EpochMillis) * time.Millisecond):
			c.epochChan <- struct{}{}
		case <-c.shutdownEpochChan:
			LOGV.Println("Epoch Handler shutdown!")
			return
		}
	}
}

/*
 * helper functions
 */

// marshalling and send message via udp connection
func (c *client) sendMsg(msg *Message) {
	packet, _ := json.Marshal(msg)
	c.conn.Write(packet)
}

func (c *client) getMinExpectedSeqId() int {
	if c.readRequestQueue.Len() > 0 {
		return c.readRequestQueue.Front().Value.(*readRequest).expectedSeqId
	}
	return c.expectedSeqId
}

func (c *client) handleReadRequest(request *readRequest) {
	// first check whether the connection is closed or lost
	if c.isClosed {
		close(request.response)
	} else {
		// TODO recheck
		request.expectedSeqId = c.expectedSeqId
		c.expectedSeqId++

		// check whether requested msg is in buf
		// if yes, grab and go, also remember to update sequence ids..
		// possibly delete some entries in bufmap
		if msg, ok := c.receivedMsgBuf[request.expectedSeqId]; ok {
			request.response <- msg.Payload
			if msg.SeqNum <= c.maxReceivedSeqId-c.params.WindowSize {
				delete(c.receivedMsgBuf, msg.SeqNum)
			}
		} else {
			// if lost and no satisfied msg to read
			if c.isLost {
				close(request.response)
			}
			// enque
			c.readRequestQueue.PushBack(request)
		}
	}
}

func (c *client) handleWriteRequest(request *writeRequest) {
	// first check if the connection has been lost
	if c.isLost {
		close(request.response)
	} else {
		// notify Write that request accpepted
		request.response <- struct{}{}

		// send data message only if it's within the sliding window
		dataMsg := NewData(c.connId, c.clientSeqId, request.payload)
		c.clientSeqId++
		if c.sentMsgBuf.Len() == 0 ||
			c.sentMsgBuf.Front().Value.(*Message).SeqNum+c.params.WindowSize >
				dataMsg.SeqNum {
			c.sendMsg(dataMsg)
		}

		// add data message to sentMsgBuf
		c.sentMsgBuf.PushBack(dataMsg)
	}
}

func (c *client) handleCloseRequest(request *closeRequest) {
	LOGV.Println("Close request received")
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
	LOGV.Println("Client receive new msg", msg)
	c.recvMsgLastEpoch = true
	if msg.Type == MsgAck {
		if c.expectedSeqId == 0 {
			c.connId = msg.ConnID
			// update status
			c.expectedSeqId = 1
			c.maxReceivedSeqId = 0
			c.clientSeqId = 1

			// notify NewClient
			c.connAckChan <- struct{}{}
		} else {
			// check against sentMsgBuf
			// TODO: shall we send pending msgs here? or waiting for epoch

			oldWindow := -1
			for e := c.sentMsgBuf.Front(); e != nil; e = e.Next() {
				if msg.SeqNum == e.Value.(*Message).SeqNum {
					if e == c.sentMsgBuf.Front() {
						oldWindow = msg.SeqNum + c.params.WindowSize
					}
					c.sentMsgBuf.Remove(e)
					break
				}
			}
			//send pending message, not waiting epoch
			if oldWindow > 0 && c.sentMsgBuf.Len() > 0 {
				newWindow := c.sentMsgBuf.Front().Value.(*Message).SeqNum +
					c.params.WindowSize
				for e := c.sentMsgBuf.Front(); e != nil; e = e.Next() {
					m := e.Value.(*Message)
					if m.SeqNum >= oldWindow && m.SeqNum < newWindow {
						c.sendMsg(m)
					}
				}
			}

			// check whether there is pending close request
			if c.closeRequestQueue.Len() > 0 && c.sentMsgBuf.Len() == 0 {
				c.notifyClose()
			}
		}
	} else if msg.Type == MsgData {
		// handle data messages
		// first check duplication
		if _, present := c.receivedMsgBuf[msg.SeqNum]; !present &&
			msg.SeqNum >= c.getMinExpectedSeqId() {
			// ack
			c.sendMsg(NewAck(c.connId, msg.SeqNum))
			// add to buf
			c.receivedMsgBuf[msg.SeqNum] = msg
			// update client status
			if msg.SeqNum > c.maxReceivedSeqId {
				// remove some entries in map
				for id := c.maxReceivedSeqId - c.params.WindowSize + 1; id <=
					msg.SeqNum-c.params.WindowSize &&
					id < c.getMinExpectedSeqId(); id++ {
					if _, present := c.receivedMsgBuf[id]; present {
						delete(c.receivedMsgBuf, id)
					}
				}
				c.maxReceivedSeqId = msg.SeqNum
			}
			// possibly notify read
			if c.readRequestQueue.Len() > 0 &&
				c.readRequestQueue.Front().Value.(*readRequest).expectedSeqId ==
					msg.SeqNum {
				c.notifyRead()
			}
		}

	}
}

// handle epoch event, do regular checkup
func (c *client) handleEpochEvent() {
	// check timeout
	if !c.recvMsgLastEpoch {
		c.noMsgEpochCount++
		LOGV.Printf("It's been %d epoch that no msg was received\n",
			c.noMsgEpochCount)
	} else {
		c.noMsgEpochCount = 0
	}
	c.recvMsgLastEpoch = false

	// lost
	if !c.isLost && c.noMsgEpochCount >= c.params.EpochLimit {
		c.isLost = true
		close(c.shutdownEpochChan) // stop epoch event handler
		close(c.shutdownNtwkChan)  // stop reading from ntwk
		c.epochChan = nil
		// TODO: tricky here, check again!
		if c.closeRequestQueue.Len() > 0 {
			c.notifyClose()
		}

		// if there is pending read request, notify error
		if c.readRequestQueue.Len() > 0 {
			request := c.readRequestQueue.Front().Value.(*readRequest)
			close(request.response)
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
			e.Value.(*Message).SeqNum < windowUpperLimit; e = e.Next() {
			c.sendMsg(e.Value.(*Message))
			LOGV.Println("Client Resend Data", e.Value.(*Message))
		}
	}

	// resend ACKs
	if c.maxReceivedSeqId > 0 {
		for seqId := c.maxReceivedSeqId; seqId > 0 &&
			seqId > c.maxReceivedSeqId-c.params.WindowSize; seqId-- {
			if msg, ok := c.receivedMsgBuf[seqId]; ok {
				ack := NewAck(c.connId, msg.SeqNum)
				c.sendMsg(ack)
				LOGV.Println("Client Resend ACK", ack)
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
	req.(*closeRequest).response <- struct{}{}
}

// handle pending Read requests
func (c *client) notifyRead() {
	// get called when there is pending read request
	// and also new msg received matched the expectedSeqId of the frist request

	// clear queue, deliver payload to Read
	for e := c.readRequestQueue.Front(); e != nil; {
		next := e.Next()
		if msg, ok :=
			c.receivedMsgBuf[e.Value.(*readRequest).expectedSeqId]; ok {
			e.Value.(*readRequest).response <- msg.Payload
			c.readRequestQueue.Remove(e)

			// possibly delete this entry in map if it's outside the window
			if msg.SeqNum <= c.maxReceivedSeqId-c.params.WindowSize {
				delete(c.receivedMsgBuf, msg.SeqNum)
			}
		} else {
			break
		}
		e = next
	}
}
