//go:build linux

package arpcfs

import (
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/sonroyaalmerol/pbs-plus/internal/agent/vssfs"
	"github.com/sonroyaalmerol/pbs-plus/internal/syslog"
)

func (f *ARPCFile) Close() error {
	if f.isClosed {
		return nil
	}

	if f.fs.session == nil {
		syslog.L.Error("RPC failed: aRPC session is nil")
		return os.ErrInvalid
	}

	req := vssfs.CloseReq{HandleID: f.handleID}
	reqBytes, err := req.MarshalMsg(nil)
	if err != nil {
		return os.ErrInvalid
	}

	_, err = f.fs.session.CallMsgWithTimeout(10*time.Second, f.jobId+"/Close", reqBytes)
	f.isClosed = true
	if err != nil {
		syslog.L.Errorf("Write RPC failed (%s): %v", f.name, err)
		return err
	}

	return nil
}

func (f *ARPCFile) ReadAt(p []byte, off int64) (int, error) {
	if f.isClosed {
		return 0, os.ErrInvalid
	}

	if f.fs.session == nil {
		return 0, os.ErrInvalid
	}

	req := vssfs.ReadAtReq{
		HandleID: f.handleID,
		Offset:   int64(off),
		Length:   len(p),
	}
	reqBytes, err := req.MarshalMsg(nil)
	if err != nil {
		return 0, os.ErrInvalid
	}

	bytesRead, err := f.fs.session.CallMsgWithBuffer(f.fs.ctx, f.jobId+"/ReadAt", reqBytes, p)
	if err != nil {
		syslog.L.Errorf("Read RPC failed (%s): %v", f.name, err)
		return 0, err
	}

	go atomic.AddInt64(&f.fs.totalBytes, int64(bytesRead))

	// Return EOF when fewer bytes are read than requested
	if bytesRead < len(p) {
		return bytesRead, io.EOF
	}

	return bytesRead, nil
}
