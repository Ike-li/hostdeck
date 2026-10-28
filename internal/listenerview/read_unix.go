//go:build unix

package listenerview

import "context"

const unixReader = true

var lsofArgv = []string{"/usr/sbin/lsof", "+c", "0", "-nP", "-iTCP", "-sTCP:LISTEN", "-F", "pcn"}
var psArgv = []string{"/bin/ps", "-axo", "pid=,ppid=,etime=,ucomm="}

func readPlatform(ctx context.Context, run commandRunner) ([]Socket, map[int]Proc, error) {
	if run == nil {
		return nil, nil, &readFailure{en: "Could not list listening ports.", zh: "读不到正在监听的端口。"}
	}
	lsofOut, lsofExit, lsofErr := run(ctx, append([]string(nil), lsofArgv...))
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	psOut, psExit, psErr := run(ctx, append([]string(nil), psArgv...))
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return snapshotFrom(commandOutcome{Out: lsofOut, Exit: lsofExit, Err: lsofErr}, commandOutcome{Out: psOut, Exit: psExit, Err: psErr})
}
