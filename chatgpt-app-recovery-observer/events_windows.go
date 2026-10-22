package observer

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var eventDLL = syscall.NewLazyDLL("wevtapi.dll")
var eventQuery = eventDLL.NewProc("EvtQuery")
var eventNext = eventDLL.NewProc("EvtNext")
var eventClose = eventDLL.NewProc("EvtClose")
var eventRender = eventDLL.NewProc("EvtRender")
var eventContext = eventDLL.NewProc("EvtCreateRenderContext")
var eventLog = eventDLL.NewProc("EvtOpenLog")
var eventLogInfo = eventDLL.NewProc("EvtGetLogInfo")

type eventVariant struct {
	Value uint64
	Count uint32
	Type  uint32
}

func openQuery(query string, reverse bool) (uintptr, error) {
	flags := uintptr(1 | 0x100)
	if reverse {
		flags = 1 | 0x200
	}
	channel := utf16Pointer("Application")
	filter := utf16Pointer(query)
	h, _, err := eventQuery.Call(0, uintptr(unsafe.Pointer(channel)), uintptr(unsafe.Pointer(filter)), flags)
	if h == 0 {
		return 0, err
	}
	return h, nil
}

func nextEvent(query uintptr) (uintptr, error) {
	var h uintptr
	var returned uint32
	ok, _, err := eventNext.Call(query, 1, uintptr(unsafe.Pointer(&h)), 0, 0, uintptr(unsafe.Pointer(&returned)))
	if ok == 0 {
		return 0, err
	}
	if returned != 1 || h == 0 {
		return 0, errors.New("event_query_incomplete")
	}
	return h, nil
}

func logEpoch() (string, error) {
	channel := utf16Pointer("Application")
	h, _, err := eventLog.Call(0, uintptr(unsafe.Pointer(channel)), 1)
	if h == 0 {
		return "", err
	}
	defer eventClose.Call(h)
	var v eventVariant
	var used uint32
	ok, _, err := eventLogInfo.Call(h, 0, unsafe.Sizeof(v), uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&used)))
	if ok == 0 || used != uint32(unsafe.Sizeof(v)) || v.Type != 17 || v.Value == 0 {
		return "", errors.New("log_epoch_unknown")
	}
	return Fingerprint("Application", strconv.FormatUint(v.Value, 10)), nil
}

func eventHeader(h uintptr) (uint64, string, error) {
	paths := []*uint16{utf16Pointer("Event/System/EventRecordID"), utf16Pointer("Event/System/TimeCreated/@SystemTime")}
	c, _, err := eventContext.Call(2, uintptr(unsafe.Pointer(&paths[0])), 0)
	runtime.KeepAlive(paths)
	if c == 0 {
		return 0, "", err
	}
	defer eventClose.Call(c)
	var used, count uint32
	ok, _, err := eventRender.Call(c, h, 0, 0, 0, uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&count)))
	if ok != 0 || err != syscall.ERROR_INSUFFICIENT_BUFFER || used < 32 || used > 4096 {
		return 0, "", errors.New("event_header_unknown")
	}
	buffer := make([]uint64, (used+7)/8)
	ok, _, err = eventRender.Call(c, h, 0, uintptr(len(buffer)*8), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&count)))
	if ok == 0 || count != 2 {
		return 0, "", errors.New("event_header_unknown")
	}
	values := unsafe.Slice((*eventVariant)(unsafe.Pointer(&buffer[0])), 2)
	if values[0].Type != 10 || values[1].Type != 17 {
		return 0, "", errors.New("event_header_schema_unknown")
	}
	record := values[0].Value
	return record, Fingerprint(strconv.FormatUint(record, 10), strconv.FormatUint(values[1].Value, 10)), nil
}

func newestCursor(epoch string) (Cursor, error) {
	q, err := openQuery("*", true)
	if err != nil {
		return Cursor{}, err
	}
	defer eventClose.Call(q)
	h, err := nextEvent(q)
	if err == syscall.Errno(259) {
		return Cursor{Epoch: epoch, Anchor: Fingerprint("empty")}, nil
	}
	if err != nil {
		return Cursor{}, err
	}
	defer eventClose.Call(h)
	record, anchor, err := eventHeader(h)
	return Cursor{epoch, record, anchor}, err
}

func checkAnchor(cursor Cursor) error {
	if cursor.Record == 0 {
		return nil
	}
	q, err := openQuery(fmt.Sprintf("*[System[EventRecordID=%d]]", cursor.Record), false)
	if err != nil {
		return err
	}
	defer eventClose.Call(q)
	h, err := nextEvent(q)
	if err != nil {
		return anchorResult(cursor, 0, "", err)
	}
	defer eventClose.Call(h)
	record, anchor, err := eventHeader(h)
	return anchorResult(cursor, record, anchor, err)
}

func anchorResult(cursor Cursor, record uint64, anchor string, err error) error {
	if errors.Is(err, syscall.Errno(259)) {
		return ErrLogReset
	}
	if err != nil {
		return errors.New("event_anchor_unreadable")
	}
	if record != cursor.Record || anchor != cursor.Anchor {
		return ErrLogReset
	}
	return nil
}

func renderTargetXML(h uintptr) ([]byte, error) {
	var used, count uint32
	ok, _, err := eventRender.Call(0, h, 1, 0, 0, uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&count)))
	if ok != 0 || err != syscall.ERROR_INSUFFICIENT_BUFFER || used == 0 || used > 65536 {
		return nil, errors.New("target_event_unreadable")
	}
	b := make([]uint16, (used+1)/2)
	ok, _, _ = eventRender.Call(0, h, 1, uintptr(len(b)*2), uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&count)))
	if ok == 0 {
		return nil, errors.New("target_event_unreadable")
	}
	return []byte(syscall.UTF16ToString(b)), nil
}

// The only event payload ever decoded is the bounded, fresh Application Hang
// schema. No formatted/raw message is logged or retained in state/results.
func parseHangXML(b []byte, scope windowsScope, epoch, last string, p ProcessSample, missing func(uint32, uint64) bool) (*Event, error) {
	var wire struct {
		System struct {
			Provider struct {
				Name string `xml:"Name,attr"`
			} `xml:"Provider"`
			ID      uint32 `xml:"EventID"`
			Record  uint64 `xml:"EventRecordID"`
			Created struct {
				At string `xml:"SystemTime,attr"`
			} `xml:"TimeCreated"`
		} `xml:"System"`
		Data []struct {
			Name  string `xml:"Name,attr"`
			Value string `xml:",chardata"`
		} `xml:"EventData>Data"`
	}
	if len(b) > 65536 || xml.Unmarshal(b, &wire) != nil {
		return nil, errors.New("target_event_schema_unknown")
	}
	if wire.System.Provider.Name != "Application Hang" || wire.System.ID != 1002 {
		return nil, errors.New("target_event_schema_unknown")
	}
	fields := map[string]string{}
	for _, d := range wire.Data {
		switch d.Name {
		case "AppName", "ProcessId", "StartTime", "PackageFullName", "PackageRelativeAppId":
			if _, duplicate := fields[d.Name]; duplicate {
				return nil, errors.New("duplicate_event_field")
			}
			fields[d.Name] = d.Value
		}
	}
	if !strings.EqualFold(fields["AppName"], "ChatGPT.exe") || fields["PackageFullName"] != scope.fullPackage || fields["PackageRelativeAppId"] != "App" {
		return nil, nil
	}
	pid, err := parseHex(fields["ProcessId"])
	if err != nil || pid == 0 || pid > 0xffffffff {
		return nil, errors.New("event_pid_unknown")
	}
	started, err := parseHex(fields["StartTime"])
	if err != nil || started == 0 {
		return nil, errors.New("event_start_unknown")
	}
	at, err := time.Parse(time.RFC3339Nano, wire.System.Created.At)
	if err != nil {
		return nil, errors.New("event_time_unknown")
	}
	instance := instanceFingerprint(scope.target, uint32(pid), started)
	linked := last != "" && instance == last && p.Target == scope.target
	return &Event{Epoch: epoch, Record: wire.System.Record, At: at, Instance: instance, PackageMatches: true, SessionMatches: linked,
		SchemaKnown: true, ExitVerified: linked && p.Presence == Absent && missing(uint32(pid), started)}, nil
}

func (w *WindowsSource) ReadEvents(ctx context.Context, cursor Cursor, p ProcessSample, last string) (Cursor, *Event, error) {
	if !p.ScopeKnown || p.Target != w.scope.target || !p.InScope {
		return Cursor{}, nil, errors.New("event_scope_unknown")
	}
	if err := ctx.Err(); err != nil {
		return Cursor{}, nil, err
	}
	epoch, err := logEpoch()
	if err != nil {
		return Cursor{}, nil, errors.New("event_log_unknown")
	}
	head, err := newestCursor(epoch)
	if err != nil {
		return Cursor{}, nil, errors.New("event_head_unknown")
	}
	if cursor.Epoch == "" {
		return head, nil, nil
	} // First run: baseline, never backfill.
	if cursor.Epoch != head.Epoch || head.Record < cursor.Record {
		return Cursor{}, nil, ErrLogReset
	}
	if err = checkAnchor(cursor); err != nil {
		return Cursor{}, nil, err
	}
	// Query only after the durable watermark, within TTL, fenced to this head.
	// A header-only exact anchor lookup above detects record reuse/log clear.
	filter := fmt.Sprintf("*[System[Provider[@Name='Application Hang'] and EventID=1002 and EventRecordID>%d and EventRecordID<=%d and TimeCreated[timediff(@SystemTime)<=300000]]]", cursor.Record, head.Record)
	q, err := openQuery(filter, false)
	if err != nil {
		return Cursor{}, nil, errors.New("event_query_unknown")
	}
	defer eventClose.Call(q)
	var candidate *Event
	for count := 0; count <= 32; count++ {
		if ctx.Err() != nil {
			return Cursor{}, nil, errors.New("event_query_timeout")
		}
		h, err := nextEvent(q)
		if err == syscall.Errno(259) {
			return head, candidate, nil
		}
		if err != nil {
			return Cursor{}, nil, errors.New("event_query_unknown")
		}
		if count == 32 {
			eventClose.Call(h)
			return Cursor{}, nil, errors.New("event_batch_limit")
		}
		b, renderErr := renderTargetXML(h)
		eventClose.Call(h)
		if renderErr != nil {
			return Cursor{}, nil, renderErr
		}
		e, err := parseHangXML(b, w.scope, epoch, last, p, func(pid uint32, start uint64) bool {
			if !missingGeneration(pid, start) {
				return false
			}
			w.eventPID = pid
			w.eventStart = start
			return true
		})
		if err != nil {
			return Cursor{}, nil, err
		}
		if e != nil {
			// Distinct simultaneous old generations are ambiguous. Do not choose
			// whichever record happened to be returned last.
			if candidate != nil && !strings.EqualFold(candidate.Instance, e.Instance) {
				return Cursor{}, nil, errors.New("event_generation_ambiguous")
			}
			candidate = e
		}
	}
	return Cursor{}, nil, errors.New("event_batch_limit")
}
