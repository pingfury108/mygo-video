package iptv

import (
	"strings"
	"testing"
)

const sample = `#EXTM3U x-tvg-url="http://epg.example.com"
#EXTINF:-1 tvg-id="cctv1" tvg-name="CCTV-1" tvg-logo="http://logo/cctv1.png" group-title="央视",CCTV-1 综合
http://example.com/cctv1.m3u8
#EXTINF:-1 group-title="卫视",湖南卫视
http://example.com/hunan.m3u8
#EXTINF:-1,本地台
#EXTGRP:地方
http://example.com/local.m3u8
#EXTINF:-1,重复
http://example.com/cctv1.m3u8
`

func TestParse(t *testing.T) {
	chs, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(chs) != 3 {
		t.Fatalf("got %d channels, want 3", len(chs))
	}
	first := chs[0]
	if first.Name != "CCTV-1 综合" || first.Group != "央视" || first.Logo != "http://logo/cctv1.png" {
		t.Fatalf("first = %+v", first)
	}
	if chs[0].URL != "http://example.com/cctv1.m3u8" {
		t.Fatalf("url = %q", chs[0].URL)
	}
	if chs[1].Name != "湖南卫视" || chs[1].Group != "卫视" {
		t.Fatalf("second = %+v", chs[1])
	}
	if chs[2].Group != "地方" {
		t.Fatalf("third group = %q", chs[2].Group)
	}
}

func TestParseEmpty(t *testing.T) {
	if _, err := Parse(strings.NewReader("#EXTM3U\n")); err == nil {
		t.Fatal("want an error for a playlist with no channel")
	}
}
