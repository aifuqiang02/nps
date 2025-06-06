package goroutine

import (
	"io"
	"net"
	"strings"
	"sync"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego/logs"
	"github.com/panjf2000/ants/v2"
)

func init() {
	InitTrafficManager()
}

type connGroup struct {
	src    io.ReadWriteCloser
	dst    io.ReadWriteCloser
	wg     *sync.WaitGroup
	n      *int64
	flow   *file.Flow
	task   *file.Tunnel
	remote string
}

//func newConnGroup(dst, src io.ReadWriteCloser, wg *sync.WaitGroup, n *int64) connGroup {
//	return connGroup{
//		src: src,
//		dst: dst,
//		wg:  wg,
//		n:   n,
//	}
//}

func newConnGroup(dst, src io.ReadWriteCloser, wg *sync.WaitGroup, n *int64, flow *file.Flow, task *file.Tunnel, remote string) connGroup {
	return connGroup{
		src:    src,
		dst:    dst,
		wg:     wg,
		n:      n,
		flow:   flow,
		task:   task,
		remote: remote,
	}
}

func CopyBuffer(dst io.Writer, src io.Reader, flow *file.Flow, task *file.Tunnel, remote string) (err error) {
	buf := common.CopyBuff.Get()
	defer common.CopyBuff.Put(buf)
	for {
		if len(buf) <= 0 {
			break
		}
		nr, er := src.Read(buf)

		//if len(pr)>0 && pr[0] && nr > 50 {
		//	logs.Warn(string(buf[:50]))
		//}

		if task != nil {
			task.IsHttp = false
			firstLine := string(buf[0:nr])
			if len(firstLine) > 3 {
				method := firstLine[0:3]
				if method != "" && (method == "HTT" || method == "GET" || method == "POS" || method == "HEA" || method == "PUT" || method == "DEL") {
					if method != "HTT" {
						heads := strings.Split(firstLine, "\r\n")
						if len(heads) >= 2 {
							logs.Info("HTTP Request method %s, %s, remote address %s, target %s", heads[0], heads[1], remote, task.Target.TargetStr)
						}
					}

					task.IsHttp = true
				}
			}
		}

		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				//written += int64(nw)
				if flow != nil && task != nil && task.AccountId > 0 {
					TrafficManager.AccumulateTrafficData(task.AccountId, int64(nw))
					// <<20 = 1024 * 1024
					flowLimit := TrafficManager.GetFlowLimitFromCache(task.AccountId)
					if err != nil {
						logs.Error("Failed to get flow limit for account %d: %v", task.AccountId, err)
						flowLimit = 0
					}
					if flowLimit <= 0 {
						logs.Info("流量已经超出限制 (已用: %d, 限制: %d)", flow.ExportFlow+flow.InletFlow, flowLimit)
						break
					}
				}

			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			err = er
			break
		}
	}
	//return written, err
	return err
}

func copyConnGroup(group interface{}) {
	//logs.Info("copyConnGroup.........")
	cg, ok := group.(connGroup)
	if !ok {
		return
	}
	var err error
	err = CopyBuffer(cg.dst, cg.src, cg.flow, cg.task, cg.remote)
	if err != nil {
		cg.src.Close()
		cg.dst.Close()
		//logs.Warn("close npc by copy from nps", err, c.connId)
	}

	//if conns.flow != nil {
	//	conns.flow.Add(in, out)
	//}
	cg.wg.Done()
}

type Conns struct {
	conn1 io.ReadWriteCloser // mux connection
	conn2 net.Conn           // outside connection
	flow  *file.Flow
	wg    *sync.WaitGroup
	task  *file.Tunnel
}

func NewConns(c1 io.ReadWriteCloser, c2 net.Conn, flow *file.Flow, wg *sync.WaitGroup, task *file.Tunnel) Conns {
	return Conns{
		conn1: c1,
		conn2: c2,
		flow:  flow,
		wg:    wg,
		task:  task,
	}
}

func copyConns(group interface{}) {
	//logs.Info("copyConns.........")
	conns := group.(Conns)
	wg := new(sync.WaitGroup)
	wg.Add(2)
	var in, out int64
	remoteAddr := conns.conn2.RemoteAddr().String()
	_ = connCopyPool.Invoke(newConnGroup(conns.conn1, conns.conn2, wg, &in, conns.flow, conns.task, remoteAddr))
	// outside to mux : incoming
	_ = connCopyPool.Invoke(newConnGroup(conns.conn2, conns.conn1, wg, &out, conns.flow, conns.task, remoteAddr))
	// mux to outside : outgoing
	wg.Wait()
	//if conns.flow != nil {
	//	conns.flow.Add(in, out)
	//}
	conns.wg.Done()
}

var connCopyPool, _ = ants.NewPoolWithFunc(200000, copyConnGroup, ants.WithNonblocking(false))
var CopyConnsPool, _ = ants.NewPoolWithFunc(100000, copyConns, ants.WithNonblocking(false))
