package main

import "testing"

func TestLayoutFitsLaptopClientAreas(t *testing.T) {
	// Windowed/maximized client sizes, including scaled displays.
	for _, size := range [][2]int32{{744, 421}, {900, 560}, {1040, 640}, {1144, 690}, {1340, 710}, {1900, 980}} {
		w, h := size[0], size[1]
		g := computeLayout(w, h)
		for _, b := range []box{g.OutLabel, g.OutChLabel, g.InLabel, g.InChLabel, g.Connect, g.Refresh, g.Receive, g.Send, g.Cancel, g.Rename, g.Hint, g.Source, g.Summary, g.Status} {
			if b.X < 0 || b.Y < 0 || b.X+b.W > w || b.Y+b.H > h {
				t.Fatalf("%v: control outside client: %+v", size, b)
			}
		}
		for _, b := range []box{g.Out, g.In, g.OutCh, g.InCh} {
			// Combo height includes its dropdown, not the closed field.
			if b.X < 0 || b.X+b.W > w || b.W < 50 {
				t.Fatalf("%v: clipped MIDI field %+v", size, b)
			}
		}
		for i, b := range g.Lists {
			if b.W <= 0 || b.H <= 0 || b.X+b.W > w || b.Y+b.H > g.Status.Y {
				t.Fatalf("%v: list clipped %+v", size, b)
			}
			if b.Y < g.Summary.Y+g.Summary.H {
				t.Fatal("list overlaps summary")
			}
			if i > 0 && b.X < g.Lists[i-1].X+g.Lists[i-1].W {
				t.Fatal("overlapping columns")
			}
		}
		for _, fontHeight := range []int32{13, 16, 20} {
			row := listRowHeight(g.Lists[0].H, fontHeight, 17)
			if row < fontHeight+3 {
				t.Fatal("text clipped vertically")
			}
			if g.Lists[0].H >= 32*(fontHeight+3)+21 && 32*row+21 > g.Lists[0].H {
				t.Fatal("32 rows should fit")
			}
		}
	}
}
