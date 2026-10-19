package main

import (
	"fmt"
	"reflect"
	"testing"
)

func TestOrderByPartsFolders(t *testing.T) {
	one := int64(1)
	pack := "Animes/Fairy Tail/[Judas] Fairy Tail (2009-2014) (Seasons 1-8 + OVAs) [BD 1080p][HEVC x265 10bit][Dual-Audio][Eng-Subs]"
	var list []ItemDTO
	add := func(p string) {
		list = append(list, ItemDTO{ID: int64(len(list) + 1), Path: p, Season: &one})
	}
	add("Animes/Fairy Tail/[Erai-raws] Fairy Tail (2018) - 01 ~ 25 [720p][Multiple Subtitle]/[Erai-raws] Fairy Tail (2018) - 01 [720p].mkv")
	add("Animes/Fairy Tail/[Erai-raws] Fairy Tail (2018) - 26 ~ 51 [720p][Multiple Subtitle]/[Erai-raws] Fairy Tail (2018) - 26 [720p].mkv")
	add(pack + "/[Judas] Fairy Tail (2009)/[Judas] Fairy Tail (2009) - 001.mkv")
	add(pack + "/[Judas] Fairy Tail (2009)/Extras/[Judas] Fairy Tail (2009) - NCED - 01.mkv")
	add(pack + "/[Judas] Fairy Tail (2011) - OVAs/[Judas] Fairy Tail (2011) - OVA 01.mkv")
	add(pack + "/[Judas] Fairy Tail (2014)/[Judas] Fairy Tail (2014) - 001.mkv")
	add(pack + "/[Judas] Fairy Tail (2014)/Extras/[Judas] Fairy Tail (2014) - NCED - 01.mkv")
	orderByParts(list, "Fairy Tail")
	var got []string
	for _, it := range list {
		got = append(got, fmt.Sprintf("%d:%s", it.ID, it.Part))
	}
	want := []string{"3:2009", "5:2011 OVAs", "6:2014", "1:2018", "2:2018", "4:2009 · Extras", "7:2014 · Extras"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestOrderByPartsKeepsSeasons(t *testing.T) {
	s1, s2 := int64(1), int64(2)
	list := []ItemDTO{
		{ID: 1, Path: "Animes/X/S2/x - 01.mkv", Season: &s2},
		{ID: 2, Path: "Animes/X/S1/x - 01.mkv", Season: &s1},
	}
	orderByParts(list, "X")
	if list[0].ID != 1 || list[0].Part != "" {
		t.Fatalf("temporadas nos nomes não devem ser reagrupadas: %+v", list)
	}
}
