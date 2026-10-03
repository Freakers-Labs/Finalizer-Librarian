package main

// All geometry uses Windows' logical coordinates, including the monitor work area.
type box struct{ X, Y, W, H int32 }
type uiLayout struct {
	OutLabel, Out, OutChLabel, OutCh                      box
	InLabel, In, InChLabel, InCh                          box
	Connect, Refresh, Receive, Send, Cancel, Rename, Hint box
	Source, Summary, Status                               box
	Lists                                                 [4]box
}

func computeLayout(w, h int32) uiLayout {
	var g uiLayout
	const pad int32 = 16
	buttonY, top := int32(61), int32(170)
	if w >= 1080 {
		g.OutLabel = box{16, 20, 70, 22}
		g.Out = box{86, 16, 250, 300}
		g.OutChLabel = box{343, 20, 24, 22}
		g.OutCh = box{370, 16, 56, 300}
		g.InLabel = box{443, 20, 60, 22}
		g.In = box{503, 16, 250, 300}
		g.InChLabel = box{760, 20, 24, 22}
		g.InCh = box{787, 16, 56, 300}
		g.Connect = box{856, 15, 110, 29}
		g.Refresh = box{976, 15, 80, 29}
		g.Hint = box{610, 66, w - 626, 22}
		g.Source = box{16, 102, w - 32, 24}
		g.Summary = box{16, 130, w - 32, 32}
	} else {
		// Fold MIDI IN onto a second row rather than clipping the right controls.
		portWidth := w - 350
		g.OutLabel = box{16, 20, 70, 22}
		g.Out = box{86, 16, portWidth, 300}
		g.OutChLabel = box{w - 254, 20, 24, 22}
		g.OutCh = box{w - 226, 16, 56, 300}
		g.InLabel = box{16, 58, 70, 22}
		g.In = box{86, 54, portWidth, 300}
		g.InChLabel = box{w - 254, 58, 24, 22}
		g.InCh = box{w - 226, 54, 56, 300}
		g.Connect = box{w - 140, 15, 124, 29}
		g.Refresh = box{w - 140, 53, 124, 29}
		buttonY = 99
		top = 224
		g.Hint = box{16, 137, w - 32, 22}
		g.Source = box{16, 163, w - 32, 24}
		g.Summary = box{16, 189, w - 32, 32}
	}
	g.Receive = box{16, buttonY, 104, 30}
	g.Send = box{130, buttonY, 104, 30}
	g.Cancel = box{244, buttonY, 80, 30}
	g.Rename = box{364, buttonY, 104, 30}
	// Never force a list beyond the client area; native scrollbars expose hidden rows.
	width := (w - 2*pad - 3*5) / 4
	for i := range g.Lists {
		g.Lists[i] = box{pad + int32(i)*(width+5), top, width, max(32, h-top-62)}
	}
	g.Status = box{16, h - 48, w - 32, 40}
	return g
}

// Reserve scrollbar height conservatively so all 32 rows fit when space permits.
func listRowHeight(height, textHeight, scrollHeight int32) int32 {
	return max(textHeight+3, (height-4-scrollHeight)/32)
}
