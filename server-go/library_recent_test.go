package main

import "testing"

func TestRecentByFolder(t *testing.T) {
	in := []ItemDTO{
		{Path: "T/.Mangás/Obra/Cap 1/27.jpg", Root: "T", Kind: "image"},
		{Path: "T/Pack/capa.jpg", Root: "T", Kind: "image"},
		{Path: "T/Pack/video.mp4", Root: "T", Kind: "video"},
		{Path: "T/Pack/outro.mp4", Root: "T", Kind: "video"},
		{Path: "T/solto1.mp4", Root: "T", Kind: "video"},
		{Path: "T/solto2.mp4", Root: "T", Kind: "video"},
	}
	got := recentByFolder(in, 3)
	want := []string{"T/Pack/video.mp4", "T/solto1.mp4", "T/solto2.mp4"}
	if len(got) != len(want) {
		t.Fatalf("got %d itens: %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].Path != w {
			t.Errorf("[%d] = %s, quer %s", i, got[i].Path, w)
		}
	}
}
