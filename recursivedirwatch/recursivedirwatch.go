// Package recursivedirwatch uses inotify to watch a directory recursively for file creation, deletion, moving or attribute changes
// subdirectories added while this is running are also watched
// Does extra file checking to catch up when speed of adding directories outpaces speed of adding watches
// Returns inotify events with the watch descriptor replaced with path of edited directory

// When Dirwatch receives an inotify event:
// 	if the created/removed/attribd file is not a directory, it sends an event for the parent directory
// 	if it is a directory:
// 		if the event is a create or moveto, adds a watch on the directory and all new subdirectories recursively,
// 			and sends events for the parent directory, the directory and all new subdirectories recursively
//		if the event is a delete or movedfrom, removes watches from the directory and all its subdirectories recursively
//		if the event is an ignored, removes wataches from the directory and all its subdirectories if the directory is still being watched

// works better than inotifywait for pasting large, deep directory

package recursivedirwatch

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmk11/lukshulibgnulinux/inotify"
	///home/JM/go/src/lukeshuinotifyedit/inotify"
)

// Event represents an inotify event on the directory in Dirname
type Event struct {
	Dirpath string       // Name of directory of altered file
	Mask    inotify.Mask // Mask describing event
	Cookie  uint32       // Unique cookie associating related events (for rename(2))
	Name    *string      // Optional name of altered file. Not always present on IN_ATTRIB changes.
}

var watches map[inotify.Wd]string
var inot *inotify.Inotify
var sendoninitial bool = false // name
var ch chan Event
// these need to be global so walkFn for filepath.Walk() can access them

// stop using this inotify package and write it single threaded
// single goroutine - since goroutines are user level threads

// if a directory with deep subdirectories and files is pasted, you probably won't get to put watches on the subdirectories
// before their subdirectories are created

// Watch must be run as a goroutine.
//
// Watch will recursively watch basedir and all its subdirectories, including newly added ones
// Whenever a file or directory is created, deleted, moved in, moved out or has its attributes edited, an Event for its parent directory will be pushed into ch.
// Some events, representing new subdirectories B, C that were added to a new directory A before directory A could be watched,
// are created by this package ('manufactured events'), not by inotify. These are marked with a mask value of 0.
// if sendoninitial is true, a manufactured event will be sent for each directory discovered in the initial filepath walk.
// will close the channel and finish only on error.
func Watch(basedir string, _ch chan Event, _sendoninitial bool) {
	var err error
	ch = _ch
	sendoninitial = _sendoninitial

	inot, err = inotify.InotifyInit()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer inot.Close()

	//var watches []inotify.Wd
	//buildWatches(basedir)
	watches = make(map[inotify.Wd]string)
	// filepath.Walk doesn't follow symbolic links, that may be a problem
	// If I want files outside the webserver files folder to be accessible through symbolic links
	// which atm I don't
	err = filepath.Walk(basedir, walkAddWatch)
	if err != nil {
		fmt.Println(err)
	} else {
		for err == nil {
			fmt.Println("Blocking on reading watches...")
			event, err := inot.ReadBlock()
			printEvent(event)
			if err == nil {
				/*
					direvent := readevent(event)
					//fmt.Printf("%x", direvent)
					if direvent != nil {
						ch <- *direvent
					}
				*/
				readevent(event, ch)
			}
		}
		fmt.Println(err)
	}
	close(ch)
	fmt.Printf("\n\nEXITING DIRWATCH AND ABOUT TO DO DEFERS\n\n")
}

func failIf(condition bool, message string) {
	if condition {
		panic(message)
	}
}

// panic with message failmessage if condition is not true
func assert(condition bool, failmessage string) {
	if !condition {
		panic(failmessage)
	}
}

// returns true if dirname startswith basedir, else false
func isSubdir(basedir string, dirname string) bool {
	return strings.HasPrefix(dirname, basedir)
}

// Delete watches from watches map and inotify watchlist
// Deletes all watches inside basedir, including basedir
// Maybe I should make a second hash table hashed in the opposite direction
// prob not worth it
func deleteWatches(watches map[inotify.Wd]string, basedir string) {
	// can't do with filepath.Walk() because the directories don't exist anymore...
	// so have to do based on file names
	for wd, dirname := range watches {
		if isSubdir(basedir, dirname) {
			delete(watches, wd)
			inot.RmWatch(wd) // probably not necessary, should be removed automatically by inotify
		}
	}
}

// Process event, creating or deleting watches on directory and its subdirectories if event is on a directory
// And convert event to DirEvent
// doesn't need to return event atm
func readevent(event inotify.Event, _ch chan Event) *Event {
	var err error
	var changedDir string
	var files []os.FileInfo
	changedDir, ok := watches[event.Wd]
	fmt.Println(changedDir)
	//if event.Mask&inotify.IN_IGNORED != 0 || event.Mask&inotify.IN_MOVE_SELF != 0 || event.Mask&inotify.IN_DELETE_SELF != 0 {
	if event.Mask&inotify.IN_IGNORED != 0 {
		// selfs are dealt with by delete_from, moved_from in parent
		// not dealing with root itself being moved atm
		// the only way this will happen that isn't dealt with elsewhere is if filesystem is unmounted afaik
		// probably caused by directory being deleted
		// watch doesn't need to be removed, remove automatically from inotify
		// but remove from watches map
		// results in doubling because when I delete the watch, in_ignored is generated
		if ok { // subdirs not already removed
			// race condition? could watches[event.Wd] already have been reset?
			// no becuase we received this notification so the wd is still being used for the ignored directory
			fmt.Println("Deleting:", changedDir)
			deleteWatches(watches, changedDir)
			// I could just delete here instead of using if delete block below
			// But I have an idea that in earlier testing IN_IGNORED seemed unreliable
		}
	} else {
		assert(ok, "watches[event.Wd] not ok in readevent(). changedDir = "+changedDir)
		if event.Mask&inotify.IN_ISDIR != 0 && event.Mask&inotify.IN_ATTRIB == 0 { // edited file is a directory
			endDir := changedDir + "/" + *event.Name
			if event.Mask&inotify.IN_DELETE != 0 || event.Mask&inotify.IN_MOVED_FROM != 0 {
				// directory deleted, unwatch it and its subdirs
				fmt.Println("Deleting:", endDir)
				deleteWatches(watches, endDir)
			}
			if event.Mask&inotify.IN_CREATE != 0 || event.Mask&inotify.IN_MOVED_TO != 0 {
				// directory created, possibly copied over with subdirs, so watch it and its subdirs
				// need to create html files on subdirs based on this information
				// because this doesn't necessarily receive information about files copied within directory
				// if lots of files and folders are copied at once
				files, err = ioutil.ReadDir(endDir)
				if err == nil {
					fmt.Println("Watching new dirs:", endDir)
					watchNewDirs(watches, endDir, files, _ch) // also adds events to ch for each new directory including endDir
				} else {
					fmt.Println(err)
				}
			}
		}
		// sometimes attrib has name
		// send event
		var direvent Event = Event{changedDir, event.Mask, event.Cookie, event.Name}
		_ch <- direvent
		return &direvent // Go will make a copy or put this on the heap or w/e right?
	}
	return nil
}

/* func sendEvent(dirname string, mask inotify.Mask, cookie uint32, name *string, ch chan Event) {
	var direvent Event = Event{dirname, mask, cookie, name}
	ch <- direvent
} */

// print an inotify.Event for debugging
func printEvent(event inotify.Event) {
	var name string
	if event.Name == nil {
		name = "nil"
	} else {
		name = *event.Name
	}
	fmt.Println(event.Wd, event.Mask, event.Cookie, name)
}

// slow
func manufactureEvent(dirpath string) Event{
	return Event{dirpath, 0, 0, nil}
}

// Add watch for given directory and any subdirectories, recursively
// Also send a manufactured event for each newly watched directory
// one inotify event implies only one new dir, right?
// wait this should be using filepath walk
// this may be faster?
func watchNewDirs(watches map[inotify.Wd]string, changedDir string, files []os.FileInfo, ch chan Event) {
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
func mapGetKey(m map[inotify.Wd]string, v string) *inotify.Wd {
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
	if info.IsDir() {
		if sendoninitial {
			ch <- manufactureEvent(path)
		}
		return addWatch(watches, path)
	}
	return nil
}

// Add a watch on path to inotify and to watches
func addWatch(watches map[inotify.Wd]string, path string) error {
	wd, err := inot.AddWatch(path, inotify.IN_CREATE|inotify.IN_ATTRIB|inotify.IN_DELETE /*| inotify.IN_MODIFY*/ |inotify.IN_MOVED_TO|inotify.IN_MOVED_FROM|inotify.IN_IGNORED|inotify.IN_ONLYDIR)
	if err != nil {
		return err
	}
	watches[wd] = path
	return nil
}
