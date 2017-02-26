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

import "sync"
import "labrpc"

import "time"
import "math/rand"
// import "bytes"
// import "encoding/gob"



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

const (
	Follower = "Follower"
	Candidate = "Candidate"
	Leader = "Leader"
)
//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex
	peers     []*labrpc.ClientEnd
	persister *Persister
	me        int // index into peers[]

	// Your data here.
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	currentTerm   int
	votedFor      int
	logs          []*LogEntry

	commitIndex   int
	lastApplied   int

	nextIndex     int
	matchIndex    int

	state         string
	heartbeatCh   chan bool
	leaderCh      chan bool
	voteCount     int
}

type LogEntry struct {
	term    int
	command interface{}
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	// Your code here.

	term = rf.currentTerm
	isleader = (rf.state == Leader)
	return term, isleader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here.
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
	// Your code here.
	// Example:
	// r := bytes.NewBuffer(data)
	// d := gob.NewDecoder(r)
	// d.Decode(&rf.xxx)
	// d.Decode(&rf.yyy)
}

type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term    int
	Success bool
}

func (rf *Raft) AppendEntries(args AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.heartbeatCh <- true

	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.Success = false
		return
	}

	if rf.state == Candidate {
		rf.state = Follower
	}

	if rf.state == Leader && args.Term > rf.currentTerm {
		rf.state = Follower
	}

	rf.currentTerm = args.Term
	reply.Success = true
}

func (rf *Raft) sendAppendEntries(server int, args AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	if ok {
		if !reply.Success && rf.currentTerm < reply.Term {
			rf.state = Follower
			rf.leaderCh <- false
		}
	}
	return ok
}

//
// example RequestVote RPC arguments structure.
//
type RequestVoteArgs struct {
	// Your data here.
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

//
// example RequestVote RPC reply structure.
//
type RequestVoteReply struct {
	// Your data here.
	Term        int
	VoteGranted bool
}

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here.
	if args.Term < rf.currentTerm {
		reply.VoteGranted = false
		reply.Term = rf.currentTerm
		DPrintf("RequestVote Error: args.Term < rf.currentTerm")
		return
	}

	if args.Term == rf.currentTerm {
		if rf.votedFor != -1 && rf.votedFor != args.CandidateId {
			reply.VoteGranted = false
			DPrintf("RequestVote Error: votedFor != -1 && votedFor != CandidateId")
			return
		}
	}

	reply.VoteGranted = true
	rf.votedFor = args.CandidateId
	rf.currentTerm = args.Term
	rf.state = Follower
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
// returns true if labrpc says the RPC was delivered.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) sendRequestVote(server int, args RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	if ok {
		if reply.VoteGranted {
			rf.voteCount++
			if rf.voteCount > len(rf.peers) / 2 {
				rf.state = Leader
				rf.leaderCh <- true
			}
		}
	}
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
	index := -1
	term := -1
	isLeader := true


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

func (rf *Raft) workAsFollower() {
	select {
	case <-rf.heartbeatCh:
		DPrintf("Follower %d: Heartbeat", rf.me)
	case <-time.After(time.Duration(rand.Intn(300) + 800) * time.Millisecond):
		rf.state = Candidate
		DPrintf("Follower %d: Election Timeout", rf.me)
	}
}

func (rf *Raft) workAsCandidate() {
	rf.voteCount = 1
	rf.votedFor = rf.me
	rf.currentTerm++

	select {
	case <-rf.leaderCh:
		DPrintf("Candidate %d: Empty the leaderCh", rf.me)
	case <-rf.heartbeatCh:
		DPrintf("Candidate %d: There is already a leader", rf.me)
		rf.state = Follower
		return
	default:
	}

	go func() {
		var args RequestVoteArgs
		args.Term = rf.currentTerm
		args.CandidateId = rf.me
		for i := 0; i < len(rf.peers); i++ {
			if i != rf.me {
				var reply RequestVoteReply
				DPrintf("Candidate %d: RequestVote to %d", rf.me, i)
				rf.sendRequestVote(i, args, &reply)
				if reply.Term > rf.currentTerm {
					DPrintf("Candidate %d: There is already a leader", rf.me)
					rf.state = Follower
					break
				}
			}
		}
	}()

	select {
	case isLeader := <-rf.leaderCh:
		if isLeader {
			rf.state = Leader
			DPrintf("Candidate %d: Become the leader", rf.me)
		}
	}
}

func (rf *Raft) workAsLeader() {
	time.Sleep(10 * time.Millisecond)

	go func() {
		var args AppendEntriesArgs
		args.Term = rf.currentTerm
		args.LeaderId = rf.me
		for i := 0; i < len(rf.peers); i++ {
			if i != rf.me {
				var reply AppendEntriesReply
				rf.sendAppendEntries(i, args, &reply)
				if rf.state != Leader {
					break
				}
			}
		}
	}()
}

func (rf *Raft) work() {
	for {
		switch rf.state {
		case Follower:
			DPrintf("Term %d: %d work as follower", rf.currentTerm, rf.me)
			rf.workAsFollower()
		case Candidate:
			DPrintf("Term %d: %d work as candidate", rf.currentTerm, rf.me)
			rf.workAsCandidate()
		case Leader:
			DPrintf("Term %d: %d work as leader", rf.currentTerm, rf.me)
			rf.workAsLeader()
		}
	}
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

	// Your initialization code here.
	rf.currentTerm = 0
	rf.votedFor = -1
	rf.state = Follower
	rf.heartbeatCh = make(chan bool)
	rf.leaderCh = make(chan bool)
	rf.voteCount = 0

	go rf.work()

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())


	return rf
}