package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"fmt"
	"labrpc"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// import "bytes"
// import "encoding/gob"

const (
	STATE_FOLLOWER = iota
	STATE_CANDIDATE
	STATE_LEADER

	HBINTERVAL            = 50
	MIN_ELECTION_INTERVAL = 400
	MAX_ELECTION_INTERVAL = 500
)

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make().
//
type ApplyMsg struct {
	Index       int
	Command     interface{}
	UseSnapshot bool   // ignore for lab2; only used in lab3
	Snapshot    []byte // ignore for lab2; only used in lab3
}

type LogEntry struct {
	Term    int
	Index   int
	Command interface{}
}

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	voteFor       int
	voteAcquired  int
	state         int32
	currentTerm   int32
	applyCh       chan ApplyMsg // channel on which the tester or service expects Raft to send ApplyMsg messages.
	heartbeatCh   chan bool
	voteCh        chan struct{}
	appendCh      chan struct{}
	electionTimer *time.Timer

	log         []LogEntry
	commitIndex int
	lastApplied int
	nextIndex   []int
	matchIndex  []int
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	// Your code here (2A).
	return int(rf.currentTerm), rf.state == STATE_LEADER
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	// w := new(bytes.Buffer)
	// e := gob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	// Your code here (2C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := gob.NewDecoder(r)
	// d.Decode(&rf.xxx)
	// d.Decode(&rf.yyy)
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term        int32
	CandidateID int

	LastLogIndex int
	LastLogTerm  int
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int32
	VoteGranted bool
}

type AppendEntriesArgs struct {
	Term     int32
	LeaderID int

	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term    int32
	Success bool

	NextTrail int
}

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if args.Term < rf.currentTerm {
		reply.VoteGranted = false
		reply.Term = rf.currentTerm
		return
	} else if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.updateStateTo(STATE_FOLLOWER)
		rf.voteFor = args.CandidateID
		reply.VoteGranted = true
	} else {
		if rf.voteFor == -1 {
			rf.voteFor = args.CandidateID
			reply.VoteGranted = true
		} else {
			reply.VoteGranted = false
		}
	}

	thisLastLogTerm := rf.log[rf.getLastLogIndex()].Term
	if thisLastLogTerm > args.LastLogTerm {
		reply.VoteGranted = false
	} else if thisLastLogTerm == args.LastLogTerm {
		if rf.getLastLogIndex() > args.LastLogIndex {
			reply.VoteGranted = false
		}
	}

	if reply.VoteGranted == true {
		go func() {
			rf.voteCh <- struct{}{}
		}()
	}
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	inform := func() {
		go func() {
			rf.appendCh <- struct{}{}
		}()
	}

	rf.mu.Lock()
	defer inform()
	defer rf.mu.Unlock()

	if args.Term < rf.currentTerm {
		reply.Success = false
		reply.Term = rf.currentTerm
		return
	} else if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.updateStateTo(STATE_FOLLOWER)
		reply.Success = true
	} else {
		reply.Success = true
	}

	if args.PrevLogIndex > rf.getLastLogIndex() {
		reply.Success = false
		reply.NextTrail = rf.getLastLogIndex() + 1
		return
	}
	if args.PrevLogTerm != rf.log[args.PrevLogIndex].Term {
		reply.Success = false
		badTerm := rf.log[args.PrevLogIndex].Term
		i := args.PrevLogIndex
		for ; rf.log[i].Term == badTerm; i-- {
		}
		reply.NextTrail = i + 1
		return
	}
	conflictIdx := 1
	if rf.getLastLogIndex() < args.PrevLogIndex+len(args.Entries) {
		conflictIdx = args.PrevLogIndex + 1
	} else {
		for idx := 0; idx < len(args.Entries); idx++ {
			if rf.log[idx+args.PrevLogIndex+1].Term != args.Entries[idx].Term {
				conflictIdx = idx + args.PrevLogIndex + 1
				break
			}
		}
	}
	if conflictIdx != -1 {
		rf.log = append(rf.log[:args.PrevLogIndex+1], args.Entries...)
	}

	if args.LeaderCommit > rf.commitIndex {
		if args.LeaderCommit < rf.getLastLogIndex() {
			rf.commitIndex = rf.getLastLogIndex()
		} else {
			rf.commitIndex = args.LeaderCommit
		}
	}
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	// Your code here (2B).
	index := -1
	term := -1
	isLeader := true

	term, isLeader = rf.GetState()
	if isLeader == true {
		rf.mu.Lock()
		index = len(rf.log)
		rf.log = append(rf.log, LogEntry{term, index, command})
		rf.mu.Unlock()
	}

	return index, term, isLeader
}

//
// the tester calls Kill() when a Raft instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (rf *Raft) Kill() {
	// Your code here, if desired.
}

func (rf *Raft) broadcastVoteReq() {
	var args RequestVoteArgs
	args.Term = rf.currentTerm
	args.CandidateID = rf.me
	args.LastLogIndex = rf.getLastLogIndex()
	args.LastLogTerm = rf.log[args.LastLogIndex].Term

	for i := range rf.peers {
		if i == rf.me {
			continue
		}
		go func(server int) {
			var reply RequestVoteReply
			if rf.state == STATE_CANDIDATE && rf.sendRequestVote(server, &args, &reply) {
				rf.mu.Lock()
				defer rf.mu.Unlock()

				if reply.VoteGranted == true {
					rf.voteAcquired++
				} else {
					if reply.Term > rf.currentTerm {
						rf.currentTerm = reply.Term
						rf.updateStateTo(STATE_FOLLOWER)
					}
				}
			} else {
				fmt.Printf("Server %d send vote req failed.\n", rf.me)
			}
		}(i)
	}
}

func (rf *Raft) broadcastAppendEntries() {
	sendAppendEntriesTo := func(server int) bool {
		var args AppendEntriesArgs
		rf.mu.Lock()
		args.Term = rf.currentTerm
		args.LeaderID = rf.me
		args.LeaderCommit = rf.commitIndex
		args.PrevLogIndex = rf.nextIndex[server] - 1
		args.PrevLogTerm = rf.log[args.PrevLogIndex].Term
		if rf.getLastLogIndex() >= rf.nextIndex[server] {
			args.Entries = rf.log[rf.nextIndex[server]:]
		}
		rf.mu.Unlock()

		var reply AppendEntriesReply

		if rf.state == STATE_LEADER && rf.sendAppendEntries(server, &args, &reply) {
			rf.mu.Lock()
			defer rf.mu.Unlock()
			if reply.Success == true {
				rf.nextIndex[server] += len(args.Entries)
				rf.matchIndex[server] = rf.nextIndex[server] - 1
			} else {
				if rf.state != STATE_LEADER {
					return false
				}
				if reply.Term > rf.currentTerm {
					rf.currentTerm = reply.Term
					rf.updateStateTo(STATE_FOLLOWER)
				} else {
					rf.nextIndex[server] = reply.NextTrail
					return true
				}
			}
		}
		return false
	}

	for i := range rf.peers {
		if i == rf.me {
			continue
		}
		//if i != rf.me {
		go func(server int) {
			for {
				if sendAppendEntriesTo(server) == false {
					break
				}
			}
		}(i)
		//}
	}
}

func (rf *Raft) updateStateTo(state int) {
	// atomic operation
	if int(rf.state) == state {
		return
	}

	stateDesc := []string{"STATE_FOLLOWER", "STATE_CANDIDATE", "STATE_LEADER"}
	preState := rf.state

	switch state {
	case STATE_FOLLOWER:
		rf.state = STATE_FOLLOWER
		rf.voteFor = -1
	case STATE_CANDIDATE:
		rf.state = STATE_CANDIDATE
		rf.startElection()
	case STATE_LEADER:
		for i := range rf.peers {
			rf.nextIndex[i] = rf.getLastLogIndex() + 1
			rf.matchIndex[i] = 0
		}
		rf.state = STATE_LEADER
	default:
		fmt.Printf("Warning: invalid state %d, do nothing.\n", state)
	}
	fmt.Printf("In term %d: Server %d transfer from %s to %s\n",
		rf.currentTerm, rf.me, stateDesc[preState], stateDesc[rf.state])
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C).
	rf.state = STATE_FOLLOWER
	rf.voteFor = -1
	rf.voteCh = make(chan struct{})
	rf.appendCh = make(chan struct{})

	rf.nextIndex = make([]int, len(rf.peers))
	rf.matchIndex = make([]int, len(rf.peers))
	rf.applyCh = applyCh
	rf.log = make([]LogEntry, 1)

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	go rf.startLoop()

	return rf
}

func (rf *Raft) startLoop() {
	rf.electionTimer = time.NewTimer(randElectionDuration())
	for {
		switch atomic.LoadInt32(&rf.state) {
		case STATE_FOLLOWER:
			select {
			case <-rf.voteCh:
				rf.electionTimer.Reset(randElectionDuration())
			case <-rf.appendCh:
				rf.electionTimer.Reset(randElectionDuration())
			case <-rf.electionTimer.C:
				rf.mu.Lock()
				rf.updateStateTo(STATE_CANDIDATE)
				rf.mu.Unlock()
			}
		case STATE_CANDIDATE:
			rf.mu.Lock()
			select {
			case <-rf.appendCh:
				rf.updateStateTo(STATE_FOLLOWER)
			case <-rf.electionTimer.C:
				rf.electionTimer.Reset(randElectionDuration())
				rf.startElection()
			default:
				if rf.voteAcquired > len(rf.peers)/2 {
					rf.updateStateTo(STATE_LEADER)
				}
			}
			rf.mu.Unlock()
		case STATE_LEADER:
			rf.broadcastAppendEntries()
			rf.updateCommitIndex()
			time.Sleep(HBINTERVAL * time.Millisecond)
		}
		go rf.applyLog()
	}
}

func (rf *Raft) updateCommitIndex() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	for i := rf.getLastLogIndex(); i > rf.commitIndex; i-- {
		matchedCount := 1
		for i, matched := range rf.matchIndex {
			if i == rf.me {
				continue
			}
			if matched > rf.commitIndex {
				matchedCount++
			}
		}
		if matchedCount > len(rf.peers)/2 {
			rf.commitIndex = i
			break
		}
	}
}

func (rf *Raft) applyLog() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.commitIndex > rf.lastApplied {
		for i := rf.lastApplied + 1; i <= rf.commitIndex; i++ {
			var msg ApplyMsg
			msg.Index = i
			msg.Command = rf.log[i].Command
			rf.applyCh <- msg
		}
	}
}

func (rf *Raft) startElection() {
	atomic.AddInt32(&rf.currentTerm, 1)
	rf.voteFor = rf.me
	rf.voteAcquired = 1
	rf.electionTimer.Reset(randElectionDuration())
	rf.broadcastVoteReq()
}

func randElectionDuration() time.Duration {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return time.Millisecond * time.Duration(r.Int63n(MAX_ELECTION_INTERVAL-MIN_ELECTION_INTERVAL)+MIN_ELECTION_INTERVAL)
}

func (rf *Raft) getLastLogIndex() int {
	return len(rf.log) - 1
}
