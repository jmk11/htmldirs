/*
Package recursivedirwatch uses inotify to watch a directory recursively for file creation, deletion, moving or attribute changes
subdirectories added while this is running are also watched
Does extra file checking to catch up when speed of adding directories outpaces speed of adding watches - if a directory with deep subdirectories and files is pasted, you probably won't get to put watches on the subdirectories before their subdirectories are created
Returns inotify events with the watch descriptor replaced with path of parent directory of edited file

When Dirwatch receives an inotify event:
	if the created/removed/attribd file is not a directory, it sends an event for the parent directory
	if it is a directory:
		create or moved_to: adds a watch on the directory and all new subdirectories recursively,
			and sends events for the parent directory, the directory and all new subdirectories recursively
		delete or moved_from: removes watches from the directory and all its subdirectories recursively
		ignored: removes wataches from the directory and all its subdirectories if the directory is still being watched

Uses filepath.Walk() to add watches initially, which doesn't follow symbolic links.
Works better (perfectly?) than inotifywait for pasting large, deep directory.

The inotify interaction, including the types, inotifyInit() and readEvent(), is adapted from Luke Shumaker's (lukeshu@sbcglobal.net) libgnulinux/inotify package (GNU LGPL 2.1+)
https://git.lukeshu.com/go/libgnulinux/
*/
package recursivedirwatch

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

type inFd int // is this worth it
type wd int
type mask uint32

// Event represents an inotify event on a file in the directory in Dirpath
// Mask, Cookie and Name are simply copied from the inotify event
type Event struct {
	Dirpath string  // Absolute path of directory of altered file
	Mask    mask    // Mask describing event
	Cookie  uint32  // Unique cookie associating related events (for rename(2))
	Name    *string // Optional name of altered file. Not always present on IN_ATTRIB changes.
}

type inotify struct {
	fd inFd

	// The bare binimum size of the buffer is
	//
	//     unix.SizeofInotifyEvent + unix.NAME_MAX + 1
	//
	// But we don't want the bare minimum.  4KiB is a page size.
	buffFull [4096]byte
	buff     []byte
	buffErr  error // change how this is done

	watches map[wd]string
}

// Watch must be run as a goroutine.
//
// Watch will recursively watch basedir and all its subdirectories, including newly added ones.
// Whenever a file or directory is created, deleted, moved in, moved out or has its attributes edited, an Event for its parent directory will be pushed into ch.
// 
// Manufactured events: Some events, representing new subdirectories B, C that were added to a new directory A before directory A could be watched,
// are created by this package, not by inotify. These are marked with a mask value of 0.
// if sendoninitial is true, a manufactured event will be sent for each directory discovered in the initial filepath walk.
// 
// Will close the channel and finish only on error. If the function exits, the channel will always be closed first.
func Watch(basedir string, ch chan Event, sendoninitial bool) {
	defer close(ch)

	inot, err := inotifyInit()
	defer unix.Close(int(inot.fd))

	inot.watches = make(map[wd]string)
	err = filepath.Walk(basedir, genAddWatchWalkFn(inot, ch, sendoninitial))
	if err != nil {
		fmt.Println(err)
		return
	}

	for err == nil {
		//fmt.Println("Blocking on reading watches...")
		event, err := inot.readEvent()
		//printEvent(event)
		if err == nil {
			processEvent(inot, event, ch)
		}
	}
	fmt.Println(err)
}

/*
func failIf(condition bool, message string) {
	if condition {
		panic(message)
	}
}
*/

// panic with message failmessage if condition is not true
/*
func assert(condition bool, failmessage string) {
	if !condition {
		panic(failmessage)
	}
}
*/

// returns true if child startswith parent, else false
func isSubdir(parent string, child string) bool {
	return strings.HasPrefix(child, parent)
}

// Process event, creating or deleting watches on directory and its subdirectories if event is on a directory.
// what if only added an event if there wasn't already an event in the queue for that directory?
// bad name
func processEvent(inot *inotify, event Event, ch chan Event) {
	if event.Mask&unix.IN_IGNORED != 0 {
		// delete_self and move_self etc. are dealt with by delete, moved_from in parent
		// the only way this will happen that isn't dealt with elsewhere is if filesystem is unmounted afaik - but I tested that and it didn't generate any IN_IGNORED
		// so I'm not sure if this code will ever run
		// watch doesn't need to be removed, inotify has already removed it automatically
		// but remove from watches map
		// results in doubling because when I delete the watch, in_ignored is generated
		// fmt.Println("Ignored -> Deleting:", event.Dirpath)
		inot.deleteWatches(event.Dirpath)
		// I could just delete here instead of using if delete block below?
	} else {
		if event.Mask&unix.IN_ISDIR != 0 /*&& event.Mask&unix.IN_ATTRIB == 0*/ { // edited file is a directory
			fullPath := event.Dirpath + "/" + *event.Name
			if event.Mask&unix.IN_DELETE != 0 || event.Mask&unix.IN_MOVED_FROM != 0 {
				// directory deleted, unwatch it and its subdirs
				//fmt.Println("Deleting:", fullPath)
				inot.deleteWatches(fullPath)
			} else if event.Mask&unix.IN_CREATE != 0 || event.Mask&unix.IN_MOVED_TO != 0 {
				// directory created, possibly copied over with subdirs, so watch it and its subdirs
				//fmt.Println("Watching new dirs:", fullPath)
				watchNewDirs(inot, fullPath, ch)
			}
		}
		// send event
		ch <- event
	}
}

/* func sendEvent(dirname string, mask inotify.Mask, cookie uint32, name *string, ch chan Event) {
	var direvent Event = Event{dirname, mask, cookie, name}
	ch <- direvent
} */

// PrintEvent prints an event for debugging
func PrintEvent(event Event) {
	var name string
	if event.Name == nil {
		name = "nil"
	} else {
		name = *event.Name
	}
	fmt.Println(event.Dirpath, event.Mask/* .String() */, event.Cookie, name)
	// printing event.Mask doesn't seem to use String()?
}

func manufactureEvent(dirpath string) Event {
	return Event{dirpath, 0, 0, nil}
}

// Add watch for given directory and any subdirectories, recursively.
// Also send a manufactured event for each newly watched directory.
// wait this should be using filepath walk. This may be faster?
// does this follow symbolic links? If so, it is inconsistent with the initial watching using filepathWalk
func watchNewDirs(in *inotify, newDir string, ch chan Event) {
	in.addWatch(newDir)
	ch <- manufactureEvent(newDir)

	files, err := ioutil.ReadDir(newDir)
	if err == nil && len(files) != 0 {
		pathprefix := newDir + "/"
		for _, file := range files {
			if file.IsDir() {
				path := pathprefix + file.Name()
				//if mapGetKey(watches, path) == nil {
				if ! mapHasValue(in.watches, path) {
					watchNewDirs(in, path, ch)
				}
			}
		}
	} else if err != nil {
		fmt.Println("watchNewDirs readDir:", err)
	}
}

// return true if map has a value == v, false if not
func mapHasValue(m map[wd]string, v string) bool {
	for _, value := range m {
		if value == v {
			return true
		}
	}
	return false
}

// deal with err?
// Returns walkFn for adding watches
func genAddWatchWalkFn(inot *inotify, ch chan Event, sendoninitial bool) func(string, os.FileInfo, error) error {
	return func(path string, file os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if file.IsDir() {
			if sendoninitial {
				ch <- manufactureEvent(path)
			}
			return inot.addWatch(path)
		}
		return nil
	}
}

// Add a watch on path to inotify and to watches
func (in *inotify) addWatch(path string) error {
	wdc, err := unix.InotifyAddWatch(int(in.fd), path, unix.IN_CREATE|unix.IN_ATTRIB|unix.IN_DELETE|unix.IN_MOVED_TO|unix.IN_MOVED_FROM|unix.IN_IGNORED|unix.IN_ONLYDIR) // unix.IN_MODIFY
	if err != nil {
		return err
	}
	in.watches[wd(wdc)] = path
	return nil
}

// Delete watches on basedir and its subdirectories (recursively) from watches map and inotify watchlist
// Maybe I should make a second hash table hashed in the opposite direction
// prob not worth it
// deleting watches in response to file deletion probably not necessary, should be removed automatically by inotify?
func (in *inotify) deleteWatches(basedir string) {
	// can't do with filepath.Walk() because the directories don't exist anymore...
	// so have to do based on file names
	for wd, dirname := range in.watches {
		if isSubdir(basedir, dirname) {
			delete(in.watches, wd)
			_, err := unix.InotifyRmWatch(int(in.fd), uint32(wd))
			if err != nil {
				// do something?
			}
			// unix.InotifyRmWatch should take wd as an int, to be consistent with AddWatch and the system call
		}
	}
}

//--------------------------------------------------------------------------
// mostly from Luke Shumaker's package
func (in *inotify) readEvent() (Event, error) {
	//START:
	if len(in.buff) == 0 { // buffer empty, fill it again
		if in.buffErr != nil {
			return Event{}, in.buffErr
		}
		var n int
		n, in.buffErr = unix.Read(int(in.fd), in.buffFull[:])
		in.buff = in.buffFull[0:n]
	}

	if len(in.buff) < unix.SizeofInotifyEvent {
		// Either Linux screwed up (and we have no chance of
		// handling that sanely), or this Inotify came from an
		// existing FD that wasn't really an inotify instance.
		// that's what Luke Shu says anyway
		in.buffErr = unix.EBADF
		return Event{}, in.buffErr
	}
	raw := (*unix.InotifyEvent)(unsafe.Pointer(&in.buff[0]))
	dirpath, ok := in.watches[wd(raw.Wd)]
	if !ok {
		// ignore this event, look for next one - it is an IN_IGNORED on a watch that has already been removed, or it is an IN_Q_OVERFLOW or ...
		// maybe shouldn't do this recursion
		// I looked at the assembly, it is compiled as a call
		in.buff = in.buff[unix.SizeofInotifyEvent+raw.Len:]
		return in.readEvent()
		//goto START
		// return Error.New("ignore event")
	}
	ret := Event{
		Dirpath: dirpath,
		Mask:    mask(raw.Mask),
		Cookie:  raw.Cookie,
		Name:    nil,
	}
	if int64(len(in.buff)) < unix.SizeofInotifyEvent+int64(raw.Len) {
		// Same as above.
		in.buffErr = unix.EBADF
		return Event{}, in.buffErr
	}
	if raw.Len > 0 { // the event has a name, of length Len
		bytes := (*[unix.NAME_MAX]byte)(unsafe.Pointer(&in.buff[unix.SizeofInotifyEvent]))
		name := strings.TrimRight(string(bytes[:raw.Len-1]), "\x00")
		ret.Name = &name
	}
	in.buff = in.buff[unix.SizeofInotifyEvent+raw.Len:] // move to next event for next call
	return ret, nil
}

// inotifyInit creates an inotify instance.
func inotifyInit() (*inotify, error) {
	fd, err := unix.InotifyInit()
	if fd < 0 {
		return nil, err
	}
	in := &inotify{
		fd: inFd(fd),
	}
	in.buff = in.buffFull[:0]
	return in, nil
}

// below copied directly from Luke Shu's package
var inBits [32]string = [32]string{
	// mask
	/*  0 */ "IN_ACCESS",
	/*  1 */ "IN_MODIFY",
	/*  2 */ "IN_ATTRIB",
	/*  3 */ "IN_CLOSE_WRITE",
	/*  4 */ "IN_CLOSE_NOWRITE",
	/*  5 */ "IN_OPEN",
	/*  6 */ "IN_MOVED_FROM",
	/*  7 */ "IN_MOVED_TO",
	/*  8 */ "IN_CREATE",
	/*  9 */ "IN_DELETE",
	/* 10 */ "IN_DELETE_SELF",
	/* 11 */ "IN_MOVE_SELF",
	/* 12 */ "(1<<12)",
	// events sent by the kernel
	/* 13 */ "IN_UNMOUNT",
	/* 14 */ "IN_Q_OVERFLOW",
	/* 15 */ "IN_IGNORED",
	/* 16 */ "(1<<16)",
	/* 17 */ "(1<<17)",
	/* 18 */ "(1<<18)",
	/* 19 */ "(1<<19)",
	/* 20 */ "(1<<20)",
	/* 21 */ "(1<<21)",
	/* 22 */ "(1<<22)",
	/* 23 */ "(1<<23)",
	// special flags
	/* 24 */ "IN_ONLYDIR",
	/* 25 */ "IN_DONT_FOLLOW",
	/* 26 */ "IN_EXCL_UNLINK",
	/* 27 */ "(1<<27)",
	/* 28 */ "(1<<28)",
	/* 29 */ "IN_MASK_ADD",
	/* 30 */ "IN_ISDIR",
	/* 31 */ "IN_ONESHOT",
}

func (msk mask) String() string {
	out := ""
	for i, name := range inBits {
		if msk&(mask(1)<<uint(i)) != 0 {
			if len(out) > 0 {
				out += "|"
			}
			out += name
		}
	}
	return out
}
