// Package web is the HTTP surface of the station: the routes of §14.5, the SSE
// stream of §13.3, the DTO of cut 4 and the authentication of ADR-018.
//
// # It owns no state that decides anything
//
// Every command becomes a domain.Event handed to the Hub, and every answer comes
// back as a domain.Ack. Nothing here reads a weight, computes a price or decides
// whether a label may be printed: a handler that could do any of that would be a
// second place where the rules live, and §13.2 has exactly one.
//
// # The DTO is DECOUPLED from the core, and that is the whole of cut 4
//
// No type of internal/domain is ever marshalled. domain.Label.NetWeight can be
// renamed without breaking the screen of a station that was not updated in the same
// hour as its service — which is what happens when a volunteer updates three
// stations out of four. The price is one conversion function, and it is frozen by a
// golden JSON test that goes red the moment a field name moves.
//
// # Three traps of the shutdown, all closed here (§13.4)
//
//  1. http.Server.Shutdown closes IDLE connections and waits for the active ones to
//     become idle. An SSE stream is active permanently, so Shutdown would burn its
//     whole budget on it, every single time a browser is connected.
//  2. r.Context() derives from Server.BaseContext, which is context.Background by
//     default: cancelling the root context does NOT cancel a request context.
//     HTTPServer sets BaseContext to the root, and that is what makes the ordered
//     shutdown of §13.4 correct.
//  3. Closing subscriber channels while publish is emitting on them is a « send on
//     closed channel ». The Hub owns that ordering; the stream handler only has to
//     leave when its channel closes, which it does.
//
// # The one place still allowed to read the real clock
//
// stream.go calls rc.SetWriteDeadline(time.Now().Add(…)). It is an I/O deadline set
// in the TCP stack of the kernel, no fake clock can drive it, and it carries no
// business decision — it bounds a write towards a browser that stopped reading.
// tools/boundary knows that file BY ITS PATH, which is why the file is named
// stream.go and stays named stream.go.
package web
