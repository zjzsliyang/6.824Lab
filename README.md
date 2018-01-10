# 6.824Lab

![go](https://img.shields.io/badge/go-1.9-blue.svg) ![coverage](https://img.shields.io/badge/coverage-96.2%25-green.svg) ![](https://img.shields.io/badge/test-9%2F9-brightgreen.svg) 

This is the lab answer for MIT 6.824 Distributed Systems(2017).

http://nil.csail.mit.edu/6.824/2017/schedule.html

## Environment

- Go 1.9

## How to Run

Set ``GOROOT`` and ``GOPATH`` correctly, and then run the following shell command.

```shell
cd 6.824Lab
export "GOPATH=$GOPATH:$PWD"
cd /src/raft
go test
```

Test A is about the leader election, keeping consistent and Test B add replicating log of operations function.

## Basics

Here is the basic roles in Raft alforithm.

- Follower
  - Respond to RPCs from candidates and leaders
  - If election timeout elapses without receiving AppendEntries RPC from current leader or granting vote to candidate: convert to candidate
- Candidate:
  - On conversion to candidate, start election:
    - Increment currentTerm
    - Vote for self
    - Reset election timer
    - Send RequestVote RPCs to all other servers
  - If votes received from majority of servers: become leader
  - If AppendEntries RPC received from new leader: convert to follower
  - If election timeout elapses: start new election
- Leader:
  - Upon election: send initial empty AppendEntries RPCs(heartbeats) to each server; repeat during idle periods to prevent election timeouts
  - If command received from client: append entry to local log, respond after entry applied to state machine
  - If last log index >= nextIndex for a follower: send AppendEntries RPC with log entries starting at nextIndex
    - If successful: update nextIndex and matchIndex for follower
    - If AppendEntries fails because of log inconsistency: decrement nextIndex and retry
  - If there exists an N such that N > commitIndex, a majority of matchIndex[i] >= N, and log[N].term  == currentTerm: set commitIndex = N

## Implementation

Since Go hasconcurreny support in the core language, it convenient to implement Raft in Go. We keep a ``Raft`` variable all the time and record the basic information and peers during the time.

### Raft Structure

Following coding shows the ``Raft`` struct. 

```go
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]

	voteFor       int
	voteAcquired  int
	state         int32
	currentTerm   int32
	applyCh       chan ApplyMsg
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
```

| variable    | meaning                                  |
| ----------- | ---------------------------------------- |
| currentTerm | lastest term server has seen (initialized to 0 on first boot, increases monotonically) |
| voteFor     | candidateID that received vote in current term (or null if none) |
| log         | log entries; each entry contains command for state machine, and term when entry was received by leader(first index is 1) |
| commitIndex | index for highest log entry known to be committed (initialized to 0, increases monotonically) |
| lastApplied | index of highest log entry applied to statemachine (initialized to 0, increases monotonically) |
| nextIndex   | for each server, index of next log entry to send to that server (initialized to leader lastlog index + 1) |
| matchIndex  | for each server, index of highest log entry known to be replicated on server (initialized to 0, increases monotonically) |

## License

MIT LICENSE