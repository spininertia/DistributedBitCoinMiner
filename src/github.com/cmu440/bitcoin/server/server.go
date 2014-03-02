package main

import (
	"container/list"
	"encoding/json"
	"fmt"
	"github.com/cmu440/bitcoin"
	"github.com/cmu440/lsp"
	"log"
	"os"
	"strconv"
)

var LOGV = log.New(os.Stdout, "VERBOSE ", log.Lmicroseconds|log.Lshortfile)

const (
	chunkSize uint64 = 10000
	maxVal    uint64 = ^uint64(0)
	idleSign  int    = -1
)

type Task struct {
	jobId   int
	lower   uint64
	upper   uint64
	minerId int
}

func newTask(jobId int, lower, upper uint64) *Task {
	return &Task{
		jobId: jobId,
		lower: lower,
		upper: upper,
	}
}

type Job struct {
	jobId int    // job Id, also the client connId
	data  string // request data field
	nonce uint64 // nonce requested

	numTasks        int           // num of sub-tasks
	numTaskFinished int           // num of finished task
	runningTasks    map[int]*Task // active tasks that is assigned to miners
	pendingTasks    *list.List    // pending tasks, value is task

	isLost bool             // indicating whether client is lost
	result *bitcoin.Message // best result so far
}

// return a new job data structure
func newJob(clientId int, data string, nonce uint64) *Job {
	job := &Job{
		jobId:           clientId,
		data:            data,
		nonce:           nonce,
		numTasks:        int((nonce + chunkSize - 1) / chunkSize),
		numTaskFinished: 0,
		runningTasks:    make(map[int]*Task),
		pendingTasks:    list.New(),
		isLost:          false,
		result:          bitcoin.NewResult(maxVal, 0),
	}

	for i := 0; i < job.numTasks; i++ {
		lower := uint64(i) * chunkSize
		var upper uint64
		if i == job.numTasks-1 {
			upper = nonce
		} else {
			upper = lower + chunkSize - 1
		}
		job.pendingTasks.PushBack(newTask(job.jobId, lower, upper))
	}
	return job
}

func (j *Job) isFinished() bool {
	return j.numTaskFinished == j.numTasks
}

func (j *Job) hasPendingTask() bool {
	return j.pendingTasks.Len() > 0
}

func (j *Job) isComplete() bool {
	return j.numTaskFinished == j.numTasks
}

func (j *Job) updateResult(minHash uint64, nonce uint64) {
	if minHash < j.result.Hash {
		j.result.Hash = minHash
		j.result.Nonce = nonce
	}
}

type Master struct {
	jobs        map[int]*Job // map from client connId to job struct
	miners      map[int]int  // map from miner connId to job id
	idleMiners  *list.List   // idle miner queue, value is miner id
	pendingJobs *list.List   // list of pending jobs
	server      lsp.Server   // server handle
}

func newMaster(server lsp.Server) *Master {
	return &Master{
		jobs:        make(map[int]*Job),
		miners:      make(map[int]int),
		idleMiners:  list.New(),
		pendingJobs: list.New(),
		server:      server,
	}
}

// handle newly arrived msg
func (m *Master) handleNewMsg(connId int, payload []byte, err error) {
	if err != nil {
		if job, present := m.jobs[connId]; present {
			// client lost
			job.isLost = true
			job.numTaskFinished += job.pendingTasks.Len()
			job.pendingTasks.Init()
		} else if _, present := m.miners[connId]; present {
			// miner lost
			m.handleLostMiner(connId)
		}

	} else {
		msg := new(bitcoin.Message)
		json.Unmarshal(payload, msg)

		switch msg.Type {
		case bitcoin.Request:
			m.handleClientRequest(connId, msg)
		case bitcoin.Result:
			m.handleMinerResult(connId, msg)
		case bitcoin.Join:
			m.handleMinerJoin(connId)
		}
	}
}

func (m *Master) handleClientRequest(connId int, msg *bitcoin.Message) {
	LOGV.Println("new client request", connId, msg)
	job := newJob(connId, msg.Data, msg.Upper)
	m.jobs[connId] = job

	// assign miners as much as possible
	for e := job.pendingTasks.Front(); e != nil; e =
		job.pendingTasks.Front() {

		if m.idleMiners.Len() == 0 {
			break
		}

		task := e.Value.(*Task)
		minerId := m.idleMiners.Front().Value.(int)

		err := m.assignTask(task, minerId)
		if err != nil {
			m.handleLostMiner(minerId)
		} else {
			job.runningTasks[minerId] = task
			m.idleMiners.Remove(m.idleMiners.Front())
			job.pendingTasks.Remove(e)
		}
	}

	// still has pending tasks
	if job.hasPendingTask() {
		m.pendingJobs.PushBack(job.jobId)
	}
}

func (m *Master) handleMinerResult(minerId int, msg *bitcoin.Message) {
	LOGV.Println("new miner result", minerId, msg)
	job := m.jobs[m.miners[minerId]]
	job.numTaskFinished++
	delete(job.runningTasks, minerId)
	if !job.isLost {
		job.updateResult(msg.Hash, msg.Nonce)

		if job.isComplete() {
			// send result to client
			payload, _ := json.Marshal(job.result)
			m.server.Write(job.jobId, payload)
		}
	}

	if job.isComplete() {
		delete(m.jobs, job.jobId)
	}

	// reassign miner
	m.handleMinerJoin(minerId)
}

func (m *Master) handleMinerJoin(minerId int) {
	LOGV.Println("new miner join", minerId)
	if m.hasPendingJobs() {
		// assign pending task to miner immediately
		jobId := m.pendingJobs.Front().Value.(int)
		job := m.jobs[jobId]
		LOGV.Println(jobId)
		task := job.pendingTasks.Remove(job.pendingTasks.Front()).(*Task)

		err := m.assignTask(task, minerId)
		if err != nil {
			m.handleLostMiner(minerId)
			job.pendingTasks.PushFront(task)
		} else {
			job.runningTasks[minerId] = task
		}

		// remove if all tasks is assigned
		if !job.hasPendingTask() {
			m.pendingJobs.Remove(m.pendingJobs.Front())
		}
	} else {
		// no pending job, add miner to idle list
		m.miners[minerId] = idleSign
		m.idleMiners.PushBack(minerId)
	}
}

func (m *Master) handleLostMiner(minerId int) {
	jobId := m.miners[minerId]

	if jobId == idleSign {
		// idle miner
		for e := m.idleMiners.Front(); e != nil; e = e.Next() {
			if e.Value.(int) == minerId {
				m.idleMiners.Remove(e)
				break
			}
		}
	} else {
		// active miner
		fmt.Println(jobId)
		job := m.jobs[jobId]
		task := job.runningTasks[minerId]
		if job.isLost {
			job.numTaskFinished++
			if job.isComplete() {
				delete(m.jobs, jobId)
			}
		} else {
			if m.idleMiners.Len() > 0 {
				// immediately assign to other idle miners
				miner := m.idleMiners.Front().Value.(int)
				err := m.assignTask(task, miner)
				if err != nil {
					m.handleLostMiner(miner)
				} else {
					job.runningTasks[minerId] = task
				}
			} else {
				// push into pending list
				if !job.hasPendingTask() {
					m.pendingJobs.PushFront(job)
				}
				job.pendingTasks.PushBack(task)
			}
		}
	}

	m.server.CloseConn(minerId)
	delete(m.miners, minerId)
}

func (m *Master) assignTask(task *Task, minerId int) error {
	msg := bitcoin.NewRequest(m.jobs[task.jobId].data,
		task.lower, task.upper)
	payload, _ := json.Marshal(msg)
	err := m.server.Write(minerId, payload)
	if err != nil {
		return err
	}

	m.miners[minerId] = task.jobId
	return nil
}

func (m *Master) hasPendingJobs() bool {
	return m.pendingJobs.Len() > 0
}

func main() {
	const numArgs = 2
	if len(os.Args) != numArgs {
		fmt.Println("Usage: ./server <port>")
		return
	}

	port, _ := strconv.Atoi(os.Args[1])
	server, _ := lsp.NewServer(port, lsp.NewParams())
	master := newMaster(server)
	for {
		connId, payload, err := server.Read()
		master.handleNewMsg(connId, payload, err)
	}
}
