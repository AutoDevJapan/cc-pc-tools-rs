package windowsrunner

import (
	"context"
	"errors"
	"net"
	"time"
)

// These numeric observations cross only the single-use, mutually pinned TLS
// session. They are not an external time endpoint or runner/source authority.
type clockRequest struct {
	Instance       string
	HostUnixMillis int64
}

type ClockReceipt struct {
	Instance                                          string
	HostUnixMillis, BeforeUnixMillis, AfterUnixMillis int64
}

func validClockMillis(ms int64) bool {
	return ms >= 1577836800000 && ms < 4102444800000 // 2020 <= UTC < 2100
}

func hostClock(c net.Conn, instance string) (ClockReceipt, error) {
	start := time.Now()
	q := clockRequest{instance, start.UnixMilli()}
	var r ClockReceipt
	if send(c, q) != nil || receive(c, &r) != nil || validateClockReceipt(q, r, start, time.Now()) != nil {
		return r, errors.New("guest UTC synchronization rejected")
	}
	return r, nil
}

func validateClockReceipt(q clockRequest, r ClockReceipt, start, end time.Time) error {
	elapsed := end.Sub(start)
	// Bound both transport uncertainty and a host wall-clock jump. A stale,
	// wrong-instance or merely echoed request cannot replace the measured ack.
	if !validHex(q.Instance, 32) || !validClockMillis(q.HostUnixMillis) || r.Instance != q.Instance || r.HostUnixMillis != q.HostUnixMillis {
		return errors.New("guest UTC identity rejected")
	}
	if !validClockMillis(r.BeforeUnixMillis) || !validClockMillis(r.AfterUnixMillis) || elapsed < 0 || elapsed > 5*time.Second || q.HostUnixMillis != start.UnixMilli() {
		return errors.New("guest UTC interval rejected")
	}
	expectedEnd := q.HostUnixMillis + elapsed.Milliseconds()
	if end.UnixMilli() < expectedEnd-1000 || end.UnixMilli() > expectedEnd+1000 || r.AfterUnixMillis < q.HostUnixMillis-1000 || r.AfterUnixMillis > expectedEnd+1000 {
		return errors.New("guest UTC measurement rejected")
	}
	return nil
}

func guestClock(ctx context.Context, c net.Conn, instance string, sync func(context.Context, time.Time) (int64, int64, error)) error {
	var q clockRequest
	if receive(c, &q) != nil || q.Instance != instance || !validClockMillis(q.HostUnixMillis) {
		return errors.New("guest UTC request rejected")
	}
	before, after, e := sync(ctx, time.UnixMilli(q.HostUnixMillis).UTC())
	if e != nil || !validClockMillis(before) || after < q.HostUnixMillis-1000 || after > q.HostUnixMillis+1000 {
		return errors.New("guest UTC update unconfirmed")
	}
	return send(c, ClockReceipt{instance, q.HostUnixMillis, before, after})
}

func updateGuestClock(ctx context.Context, target time.Time, now func() time.Time, set func(time.Time) error) (int64, int64, error) {
	if ctx.Err() != nil || !validClockMillis(target.UnixMilli()) {
		return 0, 0, errors.New("guest UTC context rejected")
	}
	before := now().UnixMilli()
	if e := set(target.UTC()); e != nil {
		return before, 0, errors.New("guest UTC update failed")
	}
	after := now().UnixMilli()
	if ctx.Err() != nil || after < target.UnixMilli()-1000 || after > target.UnixMilli()+1000 {
		return before, after, errors.New("guest UTC readback failed")
	}
	return before, after, nil
}
