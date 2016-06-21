package pbservice

import "net"
import "fmt"
import "net/rpc"
import "log"
import "time"
import "viewservice"
import "sync"
import "sync/atomic"
import "os"
import "syscall"
import "math/rand"

type PBServer struct {
	mu         sync.Mutex
	l          net.Listener
	dead       int32 // for testing
	unreliable int32 // for testing
	me         string
	vs         *viewservice.Clerk
	// Your declarations here.
	view       viewservice.View
	database   map[string]string
	clients    map[int64]string
}

func (pb *PBServer) get(args *GetArgs, reply *GetReply) {
	value, ok := pb.database[args.Key]
	if ok {
		reply.Value = value
		reply.Err = OK
	} else {
		reply.Err = ErrNoKey
	}

	pb.clients[args.Id] = reply.Value
}

func (pb *PBServer) Get(args *GetArgs, reply *GetReply) error {

	// Your code here.
	pb.mu.Lock()
	defer pb.mu.Unlock()

	v, ok := pb.clients[args.Id]
	if ok {
		reply.Value = v
		reply.Err = OK
		return nil
	}

	if pb.view.Primary != pb.me {
		reply.Err = ErrWrongServer
		return nil
	}

	if pb.view.Backup != "" {
		ok := call(pb.view.Backup, "PBServer.BackupGet", args, reply)
		if ok == false {
			return nil
		}

		if reply.Err != OK {
			return nil
		}
	}

	pb.get(args, reply)

	return nil
}

func (pb *PBServer) BackupGet(args *GetArgs, reply *GetReply) error {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	if pb.view.Backup != pb.me {
		reply.Err = ErrWrongServer
		return nil
	}

	pb.get(args, reply)

	return nil
}

func (pb *PBServer) put(args *PutAppendArgs, reply *PutAppendReply) {
	if args.Op == "Put" {
		pb.database[args.Key] = args.Value
	} else {
		value := pb.database[args.Key]
		value += args.Value
		pb.database[args.Key] = value
	}
	reply.Err = OK

	pb.clients[args.Id] = "accepted"
}

func (pb *PBServer) PutAppend(args *PutAppendArgs, reply *PutAppendReply) error {

	// Your code here.
	pb.mu.Lock()
	defer pb.mu.Unlock()

	_, ok := pb.clients[args.Id]
	if ok {
		reply.Err = OK
		return nil
	}

	if pb.view.Primary != pb.me {
		reply.Err = ErrWrongServer
		return nil
	}

	if pb.view.Backup != "" {
		ok := call(pb.view.Backup, "PBServer.BackupPutAppend", args, reply)
		if ok == false {
			return nil
		}

		if reply.Err != OK {
			return nil
		}
	}

	pb.put(args, reply)

	return nil
}

func (pb *PBServer) BackupPutAppend(args *PutAppendArgs, reply *PutAppendReply) error {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	if pb.view.Backup != pb.me {
		reply.Err = ErrWrongServer
		return nil
	}

	pb.put(args, reply)

	return nil
}

func (pb *PBServer) Copy(args *CopyArgs, reply *CopyReply) error {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	pb.database = args.Database
	pb.clients = args.Clients

	return nil
}

//
// ping the viewserver periodically.
// if view changed:
//   transition to new view.
//   manage transfer of state from primary to new backup.
//
func (pb *PBServer) tick() {

	// Your code here.
	pb.mu.Lock()
	defer pb.mu.Unlock()

	view, err := pb.vs.Ping(pb.view.Viewnum)
	if err != nil {
		return
	}

	if view.Primary == pb.me && view.Backup != "" && view.Backup != pb.view.Backup {
		args := &CopyArgs{}
		args.Database = pb.database
		args.Clients = pb.clients
		var reply CopyReply
		ok := call(view.Backup, "PBServer.Copy", args, &reply)
		if ok == false {
			return
		}
	}

	pb.view = view
}

// tell the server to shut itself down.
// please do not change these two functions.
func (pb *PBServer) kill() {
	atomic.StoreInt32(&pb.dead, 1)
	pb.l.Close()
}

// call this to find out if the server is dead.
func (pb *PBServer) isdead() bool {
	return atomic.LoadInt32(&pb.dead) != 0
}

// please do not change these two functions.
func (pb *PBServer) setunreliable(what bool) {
	if what {
		atomic.StoreInt32(&pb.unreliable, 1)
	} else {
		atomic.StoreInt32(&pb.unreliable, 0)
	}
}

func (pb *PBServer) isunreliable() bool {
	return atomic.LoadInt32(&pb.unreliable) != 0
}


func StartServer(vshost string, me string) *PBServer {
	pb := new(PBServer)
	pb.me = me
	pb.vs = viewservice.MakeClerk(me, vshost)
	// Your pb.* initializations here.
	pb.view = viewservice.View{}
	pb.database = make(map[string]string)
	pb.clients = make(map[int64]string)

	rpcs := rpc.NewServer()
	rpcs.Register(pb)

	os.Remove(pb.me)
	l, e := net.Listen("unix", pb.me)
	if e != nil {
		log.Fatal("listen error: ", e)
	}
	pb.l = l

	// please do not change any of the following code,
	// or do anything to subvert it.

	go func() {
		for pb.isdead() == false {
			conn, err := pb.l.Accept()
			if err == nil && pb.isdead() == false {
				if pb.isunreliable() && (rand.Int63()%1000) < 100 {
					// discard the request.
					conn.Close()
				} else if pb.isunreliable() && (rand.Int63()%1000) < 200 {
					// process the request but force discard of reply.
					c1 := conn.(*net.UnixConn)
					f, _ := c1.File()
					err := syscall.Shutdown(int(f.Fd()), syscall.SHUT_WR)
					if err != nil {
						fmt.Printf("shutdown: %v\n", err)
					}
					go rpcs.ServeConn(conn)
				} else {
					go rpcs.ServeConn(conn)
				}
			} else if err == nil {
				conn.Close()
			}
			if err != nil && pb.isdead() == false {
				fmt.Printf("PBServer(%v) accept: %v\n", me, err.Error())
				pb.kill()
			}
		}
	}()

	go func() {
		for pb.isdead() == false {
			pb.tick()
			time.Sleep(viewservice.PingInterval)
		}
	}()

	return pb
}
