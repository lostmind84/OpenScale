//go:build windows

package platform

// syncDirectory has nothing to flush on Windows, and the reason is worth writing down
// rather than leaving as an empty function somebody will one day « fix ».
//
// This is the twin §5.1 calls `_other.go`, the other way round: here it is the Windows
// side that cannot do the thing. FlushFileBuffers refuses a handle opened on a directory
// — ERROR_ACCESS_DENIED — so there is no call to make. What replaces it is the semantics
// of the rename itself: os.Rename goes through MoveFileEx with MOVEFILE_REPLACE_EXISTING,
// which NTFS applies as one journalled metadata transaction. The name therefore never
// points at half a file, which is the property §11.4 asks for.
//
// What this platform does NOT give is the extra durability barrier of the Unix twin: a
// power cut in the microseconds after the rename may leave the previous name in place.
// The previous name is the previous CONFIGURATION, whole and valid, so the failure mode
// is « la modification est perdue », never « le poste n'a plus de configuration ».
func syncDirectory(string) error { return nil }
