package lsp

import (
	"io/ioutil"
	"log"
	"os"
)

const (
	Bufsize = 1500
)

var LOGE = log.New(ioutil.Discard, "ERROR", log.Lmicroseconds|log.Lshortfile)
var LOGV = log.New(ioutil.Discard, "VERBOSE ", log.Lmicroseconds|log.Lshortfile)
var LOGD = log.New(os.Stdout, "DEBUG", log.Lmicroseconds|log.Lshortfile)
