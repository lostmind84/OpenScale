// This file holds WHAT IS PLUGGED IN (§14.4), and the gestures that interrogate it:
// enumerate the serial ports and the print queues, detect a scale, capture its
// frames, render a life-size label, replay a frame.
//
// Enumerating and previewing are OPEN -- looking at a port number costs nothing
// whoever stands behind the counter could not already read off the cable. Going and
// ASKING the hardware is protected: discover, detect, capture and replay all put
// something on a wire (ADR-033).
//
// Every gesture that reaches a device is bounded by deviceBudget: the volunteer
// pressing the button is standing in front of the screen.

package web

import (
	"net/http"
	"openscale/internal/station/ports"
	"time"
)

// PortInfo is one serial port the platform enumerated, with the USB description that
// makes it recognisable — « COM8 » names nothing, « COM8 — FTDI FT232R » names a
// cable somebody can see (§14.4).
type PortInfo struct {
	Name        string
	Description string
	VID         string
	PID         string
}

// PrinterInfo is one print queue or one device the platform knows about.
type PrinterInfo struct {
	Name string
	// Key is the printer.options key this destination goes into: "queue", "path" or
	// "address" (domain.DeviceKey*). The enumeration that found it is the only layer that
	// knows, and the screen has no way of telling the three apart by looking at the name.
	Key     string
	Detail  string
	Default bool
}

// ScaleDetection is what one port answered when the parsers were applied to it.
//
// It is the detection that answers « is there a scale? », not the operator (§14.4).
type ScaleDetection struct {
	Port string
	// Driver is the registry key of the parser that recognised the frames, empty when
	// none did.
	Driver     string
	ValidCount int
	// Frames is what was read, decoded, so that a support call can look at them.
	Frames  []string
	Message string
}

// PreviewQuery is what GET /admin/api/label/preview.png renders.
type PreviewQuery struct {
	Template string
	// Demo asks for the demonstration values rather than the weighing in flight,
	// which is what the settings screen shows while nobody is weighing.
	Demo bool
	// Dual asks for the two-tier layout, so that an operator sees the crowded case
	// without having to configure it first.
	Dual bool
}

// listPorts is GET /admin/api/ports.
func (s *Server) listPorts(w http.ResponseWriter, r *http.Request) {
	if s.hardware == nil {
		unavailable(w, "l'énumération des ports n'est pas câblée")
		return
	}
	ports, err := s.hardware.Ports(r.Context())
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "", err.Error())
		return
	}
	body := struct {
		Ports []portDTO `json:"ports"`
	}{make([]portDTO, 0, len(ports))}
	for _, p := range ports {
		body.Ports = append(body.Ports, portDTO{
			Name: p.Name, Description: p.Description, VID: p.VID, PID: p.PID,
		})
	}
	writeJSON(w, http.StatusOK, body)
}

// portDTO is one serial port.
type portDTO struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	VID         string `json:"vid"`
	PID         string `json:"pid"`
}

// printerDeviceDTO is one print destination this station can reach.
type printerDeviceDTO struct {
	Name string `json:"name"`
	// Key is the printer.options key this destination goes INTO, as the enumeration that
	// found it declared: "queue", "path" or "address".
	//
	// The screen writes what a volunteer clicks into THAT key and no other. It wrote every
	// one of them into `queue`, and the two routes served by this handler do not answer the
	// same kind of thing: one lists the queues of the spooler, the other the hosts that
	// replied on port 9100.
	Key     string `json:"key"`
	Detail  string `json:"detail"`
	Default bool   `json:"default"`
}

// listPrinters is GET /admin/api/printers.
func (s *Server) listPrinters(w http.ResponseWriter, r *http.Request) {
	s.answerPrinters(w, r, false)
}

// discoverPrinters is POST /admin/api/printers/discover: the deeper search, which may
// take seconds and is therefore a POST and not a GET.
func (s *Server) discoverPrinters(w http.ResponseWriter, r *http.Request) {
	s.answerPrinters(w, r, true)
}

// answerPrinters serves both printer routes.
func (s *Server) answerPrinters(w http.ResponseWriter, r *http.Request, discover bool) {
	if s.hardware == nil {
		unavailable(w, "l'énumération des imprimantes n'est pas câblée")
		return
	}
	// Enumerating a Windows spooler can take seconds, and discovering can take more.
	// A handler never waits on the platform without a deadline.
	ctx, cancel := ports.WithBudget(r.Context(), s.clock, deviceBudget)
	defer cancel()

	list, err := s.hardware.Printers(ctx)
	if discover {
		list, err = s.hardware.DiscoverPrinters(ctx)
	}
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "", err.Error())
		return
	}
	body := struct {
		Printers []printerDeviceDTO `json:"printers"`
	}{make([]printerDeviceDTO, 0, len(list))}
	for _, p := range list {
		body.Printers = append(body.Printers, printerDeviceDTO{
			Name: p.Name, Key: p.Key, Detail: p.Detail, Default: p.Default,
		})
	}
	writeJSON(w, http.StatusOK, body)
}

// detectRequest is the body of POST /admin/api/scale/detect and /scale/capture.
type detectRequest struct {
	Port string `json:"port"`
	// Seconds is how long to listen, for a capture. Zero means the default of three
	// seconds, which is what the detection of §14.4 spends on each port.
	Seconds int `json:"seconds"`
}

// detectScale is POST /admin/api/scale/detect: it opens the port, applies the parsers
// and says what answered — « COM8 : 12 trames valides, GRAM XFOC ».
func (s *Server) detectScale(w http.ResponseWriter, r *http.Request) {
	var body detectRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if s.hardware == nil {
		unavailable(w, "la détection de balance n'est pas câblée")
		return
	}
	report, err := s.hardware.DetectScale(r.Context(), body.Port)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "ERR-SCL-03", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Port       string   `json:"port"`
		Driver     string   `json:"driver"`
		ValidCount int      `json:"valid_frames_count"`
		Frames     []string `json:"frames"`
		Message    string   `json:"message"`
	}{report.Port, report.Driver, report.ValidCount, report.Frames, report.Message})
}

// captureScale is POST /admin/api/scale/capture: the raw frames, for a support call.
func (s *Server) captureScale(w http.ResponseWriter, r *http.Request) {
	var body detectRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if s.hardware == nil {
		unavailable(w, "la capture de trames n'est pas câblée")
		return
	}
	seconds := body.Seconds
	if seconds <= 0 || seconds > 60 {
		seconds = 3
	}
	frames, err := s.hardware.CaptureFrames(r.Context(), body.Port, time.Duration(seconds)*time.Second)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "ERR-SCL-03", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Frames []string `json:"frames"`
	}{frames})
}

// labelPreview is GET /admin/api/label/preview.png: the same rendering that would be
// printed, which is what A2 buys (one renderer, not two).
func (s *Server) labelPreview(w http.ResponseWriter, r *http.Request) {
	if s.hardware == nil {
		unavailable(w, "l'aperçu d'étiquette n'est pas câblé")
		return
	}
	png, err := s.hardware.LabelPreview(r.Context(), PreviewQuery{
		Template: r.URL.Query().Get("template"),
		Demo:     r.URL.Query().Get("demo") == "1",
		Dual:     r.URL.Query().Get("dual") == "1",
	})
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "", err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	// The preview is refreshed at every keystroke on the settings screen: a cached
	// one would show the previous offset.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

// replayRequest is the body of POST /admin/api/replay.
type replayRequest struct {
	// Frame is the raw frame, exactly as the journal recorded it.
	Frame string `json:"frame"`
}

// replay is POST /admin/api/replay: « Rejouer cette trame » (§14.4, Journal).
//
// It is what turns a frame that caused an unexplained refusal into a permanent test,
// without a trip to the shop and without a scale.
func (s *Server) replay(w http.ResponseWriter, r *http.Request) {
	var body replayRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if s.hardware == nil {
		unavailable(w, "le rejeu de trame n'est pas câblé")
		return
	}
	if body.Frame == "" {
		writeProblem(w, http.StatusBadRequest, "", "Aucune trame n'est fournie.")
		return
	}
	if err := s.hardware.Replay(r.Context(), body.Frame); err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, actionDTO{
		Done: true, Message: "La trame a été rejouée."})
}
