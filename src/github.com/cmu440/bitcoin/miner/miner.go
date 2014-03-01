package main

import (
	"encoding/json"
	"fmt"
	"github.com/cmu440/bitcoin"
	"github.com/cmu440/lsp"
	"os"
)

func main() {
	const numArgs = 2
	if len(os.Args) != numArgs {
		fmt.Println("Usage: ./miner <hostport>")
		return
	}

	hostport := os.Args[1]

	miner := lsp.NewClient(hostport, lsp.NewParams())
	defer miner.Close()

	joinReq := json.Marshal(bitcoin.NewJoin())
	miner.Write(joinReq)

	msg := new(bitcoin.Message)
	for {
		payload, err := miner.Read()
		if err != nil {
			break
		}
		json.Unmarshal(payload, msg)

		// mining
		minHash, nonce := mine(msg.Data, msg.Lower, msg.Upper)
		err = miner.Write(bitcoin.NewResult(minHash, nonce))

		if err != nil {
			break
		}
	}

}

func mine(data string, lower, upper int) (uint64, uint64) {
	minHash := ^uint64(0)
	var nonce uint64

	for i := lower; i <= upper; i++ {
		hash := bitcoin.Hash(data, i)
		if hash < minHash {
			minHash = hash
			nonce = i
		}
	}

	return minHash, nonce
}
