package gui

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/post"
)

type fakeTimer struct {
	stopped bool
	fire    func()
	delay   time.Duration
}

func (f *fakeTimer) Stop() bool { f.stopped = true; return true }

type harness struct {
	w        *window
	queued   chan func()
	timers   []*fakeTimer
	activity uint64
	released int
}

func setup(t *testing.T, send func(post.Message) post.Outcome) *harness {
	t.Helper()
	a := test.NewApp()
	h := &harness{queued: make(chan func(), 20)}
	h.w = newWindow(a.NewWindow("test"), config.Defaults().GUI, send, "/tmp/diagnostic.jsonl")
	h.w.dispatch = func(f func()) { h.queued <- f }
	h.w.after = func(d time.Duration, f func()) timer {
		ft := &fakeTimer{fire: f, delay: d}
		h.timers = append(h.timers, ft)
		return ft
	}
	h.w.activity = func() uint64 { return h.activity }
	h.w.release = func() { h.released++ }
	t.Cleanup(func() {
		if !h.w.closed {
			h.w.native.Close()
		}
		a.Quit()
	})
	return h
}
func (h *harness) drain(t *testing.T) {
	t.Helper()
	select {
	case f := <-h.queued:
		f()
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not deliver outcome")
	}
}
func success(post.Message) post.Outcome {
	return post.Outcome{Results: []post.SinkResult{{Name: "obsidian", Success: true}}}
}

func TestButtonsAndShortcutShareSubmissionAndRejectDuplicates(t *testing.T) {
	started := make(chan post.Message, 2)
	finish := make(chan struct{})
	h := setup(t, func(m post.Message) post.Outcome { started <- m; <-finish; return success(m) })
	if h.w.native.Canvas().Focused() != h.w.entry {
		t.Fatal("no initial focus")
	}
	h.w.entry.SetText("one\ntwo")
	test.Tap(h.w.send)
	got := <-started
	if got.Original != "one\ntwo" || !h.w.send.Disabled() {
		t.Fatal("Send did not submit and disable")
	}
	h.w.entry.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyReturn, Modifier: fyne.KeyModifierSuper})
	test.Tap(h.w.send)
	select {
	case <-started:
		t.Fatal("duplicate submitted")
	default:
	}
	close(finish)
	h.drain(t)
	if h.w.send.Disabled() {
		t.Fatal("Send remained disabled after completion")
	}
	h.w.entry.SetText("next")
	h.w.entry.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyReturn, Modifier: fyne.KeyModifierSuper})
	if (<-started).Original != "next" {
		t.Fatal("shortcut used a different message path")
	}
	h.drain(t)
	if !h.timers[0].stopped {
		t.Fatal("new submission retained old timer")
	}
	h.timers[0].fire()
	h.drain(t)
	if h.w.closed {
		t.Fatal("stale queued timer closed newer result")
	}
}

func TestInvalidMessageNeverReachesService(t *testing.T) {
	for _, value := range []string{" \n\t", string([]byte{'a', 255})} {
		t.Run(value, func(t *testing.T) {
			h := setup(t, func(post.Message) post.Outcome { t.Error("invalid message reached service"); return post.Outcome{} })
			h.w.entry.SetText(value)
			h.w.submit()
			if h.w.busy || h.w.status != 1 || h.w.result.Text == "" || len(h.timers) != 0 {
				t.Fatal("invalid rejection state")
			}
			if strings.Contains(value, "a") && !strings.Contains(h.w.result.Text, "UTF-8") {
				t.Fatal("wrong correction prompt")
			}
			h.w.close()
			if h.w.status != 1 {
				t.Fatal("rejection lost failure status")
			}
		})
	}
}

func TestResultDeadlineAndStatus(t *testing.T) {
	for _, failed := range []bool{false, true} {
		for _, interact := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{false: "success", true: "failure"}[failed], map[bool]string{false: "idle", true: "interaction"}[interact]}, "/"), func(t *testing.T) {
				h := setup(t, func(m post.Message) post.Outcome {
					o := success(m)
					if failed {
						o.Results = append(o.Results, post.SinkResult{Name: "telegram", Reason: "delivery failed", Err: errors.New("SENTINEL_SECRET")})
					}
					return o
				})
				h.w.entry.SetText("hello")
				h.w.submit()
				h.drain(t)
				wantDelay := 15 * time.Second
				wantStatus := 0
				if failed {
					wantDelay = 30 * time.Second
					wantStatus = 1
				}
				if h.timers[0].delay != wantDelay || h.w.status != wantStatus {
					t.Fatal("wrong timeout or status")
				}
				if strings.Contains(h.w.result.Text, "SENTINEL_SECRET") {
					t.Fatal("diagnostic leaked")
				}
				if failed && (!strings.Contains(h.w.result.Text, "telegram: delivery failed") || !strings.Contains(h.w.result.Text, "obsidian: sent") || !strings.Contains(h.w.result.Text, h.w.logPath)) {
					t.Fatal("partial result omitted")
				}
				if interact {
					h.activity++
				}
				h.timers[0].fire()
				h.drain(t)
				if h.w.closed == interact {
					t.Fatal("interaction did not govern deadline")
				}
				h.w.close()
				if h.w.status != wantStatus || h.released != 1 {
					t.Fatal("close lost status or released observer more than once")
				}
			})
		}
	}
}

func TestConfiguredZeroDeadlineAndCancellation(t *testing.T) {
	h := setup(t, success)
	h.w.settings.SuccessCloseSeconds = 0
	h.w.entry.SetText("hello")
	h.w.submit()
	h.drain(t)
	if h.timers[0].delay != 0 {
		t.Fatal("zero is not immediate")
	}
	h.timers[0].fire()
	h.drain(t)
	if !h.w.closed || h.w.status != 0 {
		t.Fatal("immediate success did not close")
	}
}

func TestCancelBeforeSendingDoesNotContactAnySink(t *testing.T) {
	h := setup(t, func(post.Message) post.Outcome { t.Error("cancel contacted service"); return post.Outcome{} })
	h.w.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if !h.w.closed || h.w.status != 1 {
		t.Fatal("cancel must close without claiming delivery")
	}
}

type waitingSink struct {
	name    string
	started chan struct{}
	release chan struct{}
	failed  bool
	calls   atomic.Int32
}

func (s *waitingSink) Name() string { return s.name }
func (s *waitingSink) Send(context.Context, post.Message) error {
	s.calls.Add(1)
	close(s.started)
	<-s.release
	if s.failed {
		return errors.New("diagnostic only")
	}
	return nil
}
func TestDismissalWaitsForEverySinkAndPreservesFinalStatus(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "partial"}[failed], func(t *testing.T) {
			a := &waitingSink{name: "obsidian", started: make(chan struct{}), release: make(chan struct{})}
			b := &waitingSink{name: "telegram", started: make(chan struct{}), release: make(chan struct{}), failed: failed}
			service := post.New([]post.Sink{a, b}, time.Second, nil)
			h := setup(t, service.Post)
			h.w.entry.SetText("hello")
			h.w.submit()
			<-a.started
			<-b.started
			h.w.close()
			if h.w.closed || !h.w.closing || h.released != 0 {
				t.Fatal("closed process before delivery finished")
			}
			close(a.release)
			select {
			case <-h.queued:
				t.Fatal("did not wait for sibling")
			default:
			}
			close(b.release)
			h.drain(t)
			want := 0
			if failed {
				want = 1
			}
			if !h.w.closed || h.w.status != want || a.calls.Load() != 1 || b.calls.Load() != 1 {
				t.Fatal("dismissal lost outcome")
			}
		})
	}
}

func TestEscapeWithButtonFocused(t *testing.T) {
	h := setup(t, success)
	h.w.native.Canvas().Focus(h.w.send)
	h.w.native.Canvas().Focused().TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if !h.w.closed {
		t.Fatal("button swallowed Esc")
	}
}

func TestLargeConfiguredDeadlineDoesNotWrap(t *testing.T) {
	h := setup(t, success)
	h.w.settings.SuccessCloseSeconds = math.MaxInt
	h.w.entry.SetText("hello")
	h.w.submit()
	h.drain(t)
	if h.timers[0].delay != time.Duration(math.MaxInt64) {
		t.Fatalf("deadline wrapped: %v", h.timers[0].delay)
	}
}
