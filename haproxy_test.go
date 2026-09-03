package modules

import (
	"context"
	"io"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const haproxyStatsHeader = "pxname,svname,qcur,qmax,scur,smax,slim,stot,bin,bout,dreq,dresp,ereq,econ,eresp,wretr,wredis,status,weight,act,bck,chkfail,chkdown,lastchg,downtime,qlimit,pid,iid,sid,throttle,lbtot,tracked,type,rate,rate_lim,rate_max,check_status,check_code,check_duration,hrsp_1xx,hrsp_2xx,hrsp_3xx,hrsp_4xx,hrsp_5xx,hrsp_other,hanafail,req_rate,req_rate_max,req_tot,cli_abrt,srv_abrt,mode,algo\n"

func haproxyStatRow(pxname, svname, status, weight, scur string) string {
	// fill remaining columns with placeholders matching the header shape.
	return pxname + "," + svname + ",0,0," + scur + ",0,0,0,0,0,0,0,0,0,0,0,0," + status + "," + weight + ",1,0,0,0,0,0,,1,1,1,,0,,2,0,,0,,0,,0,0,0,0,0,0,,,,,,http,roundrobin\n"
}

func TestModuleHaproxyDisable(t *testing.T) {
	socket := "/var/run/haproxy.sock"
	before := haproxyStatsHeader + haproxyStatRow("www", "web01", "UP", "1", "0")
	after := haproxyStatsHeader + haproxyStatRow("www", "web01", "MAINT", "1", "0")

	conn := &sequencedFakeConn{
		fakeConn: newFakeConn(nil),
		script: []scriptedExec{
			{cmd: "socat - UNIX-CONNECT:" + socket, stdin: "show stat\n", result: remoteexec.Result{RC: 0, Stdout: before}},
			{cmd: "socat - UNIX-CONNECT:" + socket, stdin: "show stat\n", result: remoteexec.Result{RC: 0, Stdout: before}},
			{cmd: "socat - UNIX-CONNECT:" + socket, stdin: "get weight www/web01; disable server www/web01\n", result: remoteexec.Result{RC: 0}},
			{cmd: "socat - UNIX-CONNECT:" + socket, stdin: "show stat\n", result: remoteexec.Result{RC: 0, Stdout: after}},
		},
	}
	res, err := moduleHaproxy(context.Background(), conn, map[string]any{
		"host": "web01", "backend": "www", "socket": socket, "state": "disabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleHaproxyMissingHost(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleHaproxy(context.Background(), conn, map[string]any{"state": "enabled"}); err == nil {
		t.Fatal("want error for missing host")
	}
}

func TestModuleHaproxyBadState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleHaproxy(context.Background(), conn, map[string]any{"host": "web01", "state": "bogus"}); err == nil {
		t.Fatal("want error for bad state")
	}
}

// scriptedExec is one expected Exec call for sequencedFakeConn.
type scriptedExec struct {
	cmd    string
	stdin  string
	result remoteexec.Result
}

// sequencedFakeConn is a fakeConn variant that returns scripted results
// in call order rather than keyed by command text — haproxy.go's own
// moduleHaproxy sends the identical `show stat` command several times
// with different expected replies (before vs. after acting), which
// plain fakeConn's command-keyed map cannot express.
type sequencedFakeConn struct {
	*fakeConn
	script []scriptedExec
	calls  int
}

func (s *sequencedFakeConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	if s.calls >= len(s.script) {
		return remoteexec.Result{}, nil
	}
	step := s.script[s.calls]
	s.calls++
	s.fakeConn.Commands = append(s.fakeConn.Commands, cmd)
	return step.result, nil
}
