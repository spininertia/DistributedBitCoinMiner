// define read, write and close request types

package lsp

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
		payload:  pyload,
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
