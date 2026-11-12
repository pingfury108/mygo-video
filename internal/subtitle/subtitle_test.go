package subtitle

import (
	"testing"
	"time"
)

const sampleSRT = `1
00:00:01,000 --> 00:00:04,500
第一行字幕

2
00:00:05,000 --> 00:00:08,000
第二行
和第二句
`

const sampleVTT = `WEBVTT

00:01.000 --> 00:04.500 align:start
Hello there

00:00:05.000 --> 00:08.000
Second line
`

func TestParseSRT(t *testing.T) {
	cues := Parse([]byte(sampleSRT))
	if len(cues) != 2 {
		t.Fatalf("cues %d, want 2", len(cues))
	}
	if cues[0].Start != time.Second || cues[0].End != 4500*time.Millisecond {
		t.Errorf("first cue %v–%v", cues[0].Start, cues[0].End)
	}
	if cues[0].Text != "第一行字幕" {
		t.Errorf("first text %q", cues[0].Text)
	}
	if cues[1].Text != "第二行\n和第二句" {
		t.Errorf("second text %q", cues[1].Text)
	}
}

func TestParseVTT(t *testing.T) {
	cues := Parse([]byte(sampleVTT))
	if len(cues) != 2 {
		t.Fatalf("cues %d, want 2", len(cues))
	}
	if cues[0].Start != time.Second || cues[0].Text != "Hello there" {
		t.Errorf("first cue %v %q", cues[0].Start, cues[0].Text)
	}
}

// At shows the cue the playhead is in, and nothing between cues.
func TestAt(t *testing.T) {
	cues := Parse([]byte(sampleSRT))
	if got := At(cues, 2*time.Second); got != "第一行字幕" {
		t.Errorf("at 2s: %q", got)
	}
	if got := At(cues, 4800*time.Millisecond); got != "" {
		t.Errorf("between cues: %q", got)
	}
	if got := At(cues, 6*time.Second); got != "第二行\n和第二句" {
		t.Errorf("at 6s: %q", got)
	}
}
