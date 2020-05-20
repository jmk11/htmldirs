/*
Package recursivedirwatch uses inotify to watch a directory recursively for file creation, deletion, moving or attribute changes
subdirectories added while this is running are also watched
Does extra file checking to catch up when speed of adding directories outpaces speed of adding watches
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

The inotify interaction, including the types, inotifyInit() and readEvent(), is adapted from Luke Shumaker's (lukeshu@sbcglobal.net) libgnulinux/inotify package (LGPL)
** link
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
	///home/JM/go/src/lukeshuinotifyedit/inotify"
)

type inFd int
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
	//fdBlock bool

	// The bare binimum size of the buffer is
	//
	//     unix.SizeofInotifyEvent + unix.NAME_MAX + 1
	//
	// But we don't want the bare minimum.  4KiB is a page size.
	buffFull [4096]byte
	buff     []byte
	buffErr  error
}

//var watches map[inotify.Wd]string
var watches map[wd]string
var inot *inotify

//var inot *inotify.Inotify
var sendoninitial bool = false // name
var ch chan Event
// these need to be global so walkFn for filepath.Walk() can access them

// stop using this inotify package and write it single threaded
// single goroutine - since goroutines are user level threads

// if a directory with deep subdirectories and files is pasted, you probably won't get to put watches on the subdirectories
// before their subdirectories are created

// Watch must be run as a goroutine.
//
// Watch will recursively watch basedir and all its subdirectories, including newly added ones.
// Whenever a file or directory is created, deleted, moved in, moved out or has its attributes edited, an Event for its parent directory will be pushed into ch.
// Some events, representing new subdirectories B, C that were added to a new directory A before directory A could be watched,
// are created by this package ('manufactured events'), not by inotify. These are marked with a mask value of 0.
// if sendoninitial is true, a manufactured event will be sent for each directory discovered in the initial filepath walk.
// Will close the channel and finish only on error. If the function exits, the channel will always be closed first.
func Watch(basedir string, _ch chan Event, _sendoninitial bool) {
	var err error
	ch = _ch
	sendoninitial = _sendoninitial
	defer close(ch)

	/*
		inot, err = inotify.InotifyInit()
		if err != nil {
			fmt.Println(err)
			return
		}
		defer inot.Close() // associated watches are automatically freed
	*/

	inot, err = inotifyInit()
	defer unix.Close(int(inot.fd))
	defer fmt.Printf("\n\nSTARTING DIRWATCH DEFERS\n\n")

	//var watches []inotify.Wd
	//buildWatches(basedir)
	watches = make(map[wd]string)
	// filepath.Walk doesn't follow symbolic links, that may be a problem
	// If I want files outside the webserver files folder to be accessible through symbolic links
	// which atm I don't
	err = filepath.Walk(basedir, walkAddWatch)
	if err != nil {
		fmt.Println(err)
		return
	}
	for err == nil {
		fmt.Println("Blocking on reading watches...")
		//event, err := inot.ReadBlock()
		event, err := inot.readEvent()
		printEvent(event)
		if err == nil {
			processEvent(event, ch)
			/*
				direvent := readevent(event)
				//fmt.Printf("%x", direvent)
				if direvent != nil {
					ch <- *direvent
				}
			*/
		}
	}
	fmt.Println(err)
	// close(ch)
	//fmt.Printf("\n\nEXITING DIRWATCH AND ABOUT TO DO DEFERS\n\n")
}

/*
func failIf(condition bool, message string) {
	if condition {
		panic(message)
	}
}
*/

// panic with message failmessage if condition is not true
func assert(condition bool, failmessage string) {
	if !condition {
		panic(failmessage)
	}
}

// returns true if child startswith parent, else false
func isSubdir(parent string, child string) bool {
	return strings.HasPrefix(child, parent)
}

// Delete watches from watches map and inotify watchlist
// Deletes all watches inside basedir, including basedir
// Maybe I should make a second hash table hashed in the opposite direction
// prob not worth it
func deleteWatches(watches map[wd]string, basedir string) {
	// can't do with filepath.Walk() because the directories don't exist anymore...
	// so have to do based on file names
	for wd, dirname := range watches {
		if isSubdir(basedir, dirname) {
			delete(watches, wd)
			_, err := unix.InotifyRmWatch(int(inot.fd), uint32(wd))
			if err != nil {
			}
			// unix.InotifyRmWatch should take wd as a int, to be consistent with AddWatch and the underlying system call
			//inot.RmWatch(wd)
			// probably not necessary, should be removed automatically by inotify
		}
	}
}

// Process event, creating or deleting watches on directory and its subdirectories if event is on a directory
// And convert event to DirEvent
// doesn't need to return event atm
// what if only added an event if there wasn't already an event in the queue for that directory?
// bad name
func processEvent(event Event, _ch chan Event) {
	var err error
	//var changedDir string
	var files []os.FileInfo
	//changedDir, ok := watches[event.Wd]
	fmt.Println(event.Dirpath)
	//if event.Mask&inotify.IN_IGNORED != 0 || event.Mask&inotify.IN_MOVE_SELF != 0 || event.Mask&inotify.IN_DELETE_SELF != 0 {
	/*if event.Mask&inotify.IN_Q_OVERFLOW != 0 {
		// ignore
	} else if */
	if event.Mask&unix.IN_IGNORED != 0 {
		// selfs are dealt with by delete_from, moved_from in parent
		// not dealing with root itself being moved atm
		// the only way this will happen that isn't dealt with elsewhere is if filesystem is unmounted afaik
		// probably caused by directory being deleted
		// watch doesn't need to be removed, remove automatically from inotify
		// but remove from watches map
		// results in doubling because when I delete the watch, in_ignored is generated
		/*if ok { // subdirs not already removed*/
		// race condition? could watches[event.Wd] already have been reset?
		// no becuase we received this notification so the wd is still being used for the ignored directory
		fmt.Println("Deleting:", event.Dirpath)
		deleteWatches(watches, event.Dirpath)
		// I could just delete here instead of using if delete block below
		// But I have an idea that in earlier testing IN_IGNORED seemed unreliable
	} else {
		/*} else { */
		//assert(ok, "watches[event.Wd] not ok in readevent(). changedDir = "+changedDir)
		if event.Mask&unix.IN_ISDIR != 0 && event.Mask&unix.IN_ATTRIB == 0 { // edited file is a directory
			fullPath := event.Dirpath + "/" + *event.Name
			if event.Mask&unix.IN_DELETE != 0 || event.Mask&unix.IN_MOVED_FROM != 0 {
				// directory deleted, unwatch it and its subdirs
				fmt.Println("Deleting:", fullPath)
				deleteWatches(watches, fullPath)
			}
			if event.Mask&unix.IN_CREATE != 0 || event.Mask&unix.IN_MOVED_TO != 0 {
				// directory created, possibly copied over with subdirs, so watch it and its subdirs
				// need to create html files on subdirs based on this information
				// because this doesn't necessarily receive information about files copied within directory
				// if lots of files and folders are copied at once
				files, err = ioutil.ReadDir(fullPath)
				if err == nil {
					fmt.Println("Watching new dirs:", fullPath)
					watchNewDirs(watches, fullPath, files, _ch) // also adds events to ch for each new directory including endDir
				} else {
					fmt.Println(err)
				}
			}
		}
		// sometimes attrib has name
		// send event
		//var direvent Event = Event{changedDir, event.Mask, event.Cookie, event.Name}
		_ch <- event
		//return &direvent // Go will make a copy or put this on the heap or w/e right?
	}
	//return nil
}

/* func sendEvent(dirname string, mask inotify.Mask, cookie uint32, name *string, ch chan Event) {
	var direvent Event = Event{dirname, mask, cookie, name}
	ch <- direvent
} */

// print an inotify.Event for debugging
/*
func printEvent(event inotify.Event) {
	var name string
	if event.Name == nil {
		name = "nil"
	} else {
		name = *event.Name
	}
	fmt.Println(event.Wd, event.Mask, event.Cookie, name)
}
*/

// print an inotify.Event for debugging
func printEvent(event Event) {
	var name string
	if event.Name == nil {
		name = "nil"
	} else {
		name = *event.Name
	}
	fmt.Println(event.Dirpath, event.Mask.String(), event.Cookie, name)
}

// slow
func manufactureEvent(dirpath string) Event {
	return Event{dirpath, 0, 0, nil}
}

// Add watch for given directory and any subdirectories, recursively
// Also send a manufactured event for each newly watched directory
// one inotify event implies only one new dir, right?
// wait this should be using filepath walk
// this may be faster?
// does this follow symbolic links? If so, it is inconsistent with the initial watching using filepathWalk
func watchNewDirs(watches map[wd]string, changedDir string, files []os.FileInfo, ch chan Event) {
	addWatch(watches, changedDir)
	ch <- manufactureEvent(changedDir)
	if len(files) != 0 {
		// recurse for each subdirectory
		pathprefix := changedDir + "/"
		for _, file := range files {
			if file.IsDir() {
				path := pathprefix + file.Name()
				if mapGetKey(watches, path) == nil {
					subdirfiles, err := ioutil.ReadDir(path)
					if err == nil {
						watchNewDirs(watches, path, subdirfiles, ch)
					} else {
						fmt.Println("watchNewDirs readDir:", err)
					}
				}
				// what if a new directory is moved in, with files in it? - Only one inotify IN_CREATE is given
			}
		}
	}
}

// return map key with value v, or nil if none
func mapGetKey(m map[wd]string, v string) *wd {
	for key, value := range m {
		if value == v {
			return &key
		}
	}
	return nil
}

/*
func buildWatches(directory string) error {
	watches = make(map[inotify.Wd]string)
	return filepath.Walk(directory, walkAddWatch)
}
*/

// maybe use a closure so don't have to make ch and watches global?
// deal with err?
func walkAddWatch(path string, info os.FileInfo, err error) error {
	if err != nil {
		//fmt.Println(err)
		return err
	}
	if info.IsDir() {
		if sendoninitial {
			ch <- manufactureEvent(path)
		}
		return addWatch(watches, path)
	}
	return nil
}

// Add a watch on path to inotify and to watches
func addWatch(watches map[wd]string, path string) error {
	wde, err := unix.InotifyAddWatch(int(inot.fd), path, unix.IN_CREATE|unix.IN_ATTRIB|unix.IN_DELETE|unix.IN_MOVED_TO|unix.IN_MOVED_FROM|unix.IN_IGNORED|unix.IN_ONLYDIR) // unix.IN_MODIFY
	//wd, err := inot.AddWatch(path, inotify.IN_CREATE|inotify.IN_ATTRIB|inotify.IN_DELETE /*| inotify.IN_MODIFY*/ |inotify.IN_MOVED_TO|inotify.IN_MOVED_FROM|inotify.IN_IGNORED|inotify.IN_ONLYDIR)
	if err != nil {
		return err
	}
	watches[wd(wde)] = path
	return nil
}

//--------------------------------------------------------------------------
// read a single inotify event and convert it to an Event
func (in *inotify) readEvent() (Event, error) {
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
		in.buffErr = unix.EBADF
		return Event{}, in.buffErr
	}
	raw := (*unix.InotifyEvent)(unsafe.Pointer(&in.buff[0]))
	dirpath, ok := watches[wd(raw.Wd)]
	if !ok {
		// ignore this event, look for next one
		// maybe shouldn't do this, what if get lots of duplicate IN_IGNORED and run out of stack space
		in.buff = in.buff[unix.SizeofInotifyEvent+raw.Len:]
		return in.readEvent()
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
	/*if ! ok { // ignore this event. obv a bunch of the stuff we just did was pointless but do need to skip name in buffer
		return in.readEvent()
	}*/
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

// Close closes the inotify instance; further calls to this object
// will error.
//
// In the event of a double-close condition, in go <= 1.7, this
// returns an os.SyscallError wrapping unix.EBADF; on go >= 1.8, it
// returns os.ErrClosed.
/*func (in *inotifys) close() (err error) {
	return unix.Close(int(in.fd))
}*/

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
