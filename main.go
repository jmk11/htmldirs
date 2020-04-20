package main

import (
	"fmt"
	"io/ioutil"
	"lukeshu.com/git/go/libgnulinux.git/inotify" // change line 227 of inotify.go
	"os"
	"path/filepath"
)

var watches map[inotify.Wd]string
var inot *inotify.Inotify
const outputfile = "directory.html"
// how is this intended to be dealt with

// stop using this inotify package and write it single threaded

func main() {
	var basedir string = os.Args[1]
	fmt.Println("Program is starting. Directory:", basedir)
	var err error
	inot, err = inotify.InotifyInit()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer inot.Close()
	//var watches []inotify.Wd
	//watches := make(map[inotify.Wd]string)
	buildWatches(basedir)

	var changedDir string
	for {
		fmt.Println("Blocking on reading watches...")
		event, err := inot.ReadBlock()
		if err != nil {
			fmt.Println(err)
			return
		}
		changedDir = watches[event.Wd]
		// have to check if a new directory has been added, and if so add a watch for it
		// for each member of directory, if not inMap, add watch
		files, err := ioutil.ReadDir(changedDir)
		if err != nil {
			fmt.Println(err)
			return
		}
		watchNewDirs(watches, changedDir, files)

		var html string = makeHTML(changedDir, files)
		fmt.Println(html)
		err = writeFile(changedDir+"/"+outputfile, html)
		if err != nil {
			fmt.Println(err)
			return
		}
	}
}

func watchNewDirs(watches map[inotify.Wd]string, changedDir string, files []os.FileInfo) {
	//var path string
	for _, file := range files {
		path := changedDir + "/" + file.Name()
		if file.IsDir() && ! inMap(watches, path) {
			addWatch(watches, path)
			// maybe run .html file creation on this dir right now?
			// is it possible that could have missed file creation inside the dir?
		}
	}
}

func inMap(m map[inotify.Wd]string, v string) bool {
	for _, value := range m {
		if (value == v) {
			return true
		}
	}
	return false
}

func buildWatches(directory string) (error) {
	watches = make(map[inotify.Wd]string)
	return filepath.Walk(directory, walkAddWatch)
}

/*
func writeHTML(directory string, files []os.FileInfo) error {
	
	var html string = makeHTML(directory, files)
	fmt.Println(html)
	err := writeFile(directory+"/"+outputfile, html)
	return err
}*/

func writeFile(location string, contents string) error {
	/*file, err := os.OpenFile(location, os.O_WRONLY | os.O_TRUNC, 0)
	//file, err := os.OpenFile(location, os.O_WRONLY, 0)
	if err != nil {
		file, err = os.Create(location)
		if err != nil {
			return err
		}
	}*/
	//file.Seek(0, os.SEEK_SET)
	file, err := os.Create(location) // why doesn't this cause infinite loop
	if err != nil {
		return err
	}
	_, err = file.WriteString(contents)
	if err != nil {
		return err
	}
	err = file.Close()
	if err != nil {
		return err
	}
	return nil
}

// second loop through files list
func makeHTML(directory string, files []os.FileInfo) string {
	var html string = "<html><head></head><body>\n"
	for _, file := range files {
		if file.Mode()&os.ModeSymlink == 0 && file.Name() != outputfile{
			var path string = directory + "/" + file.Name()
			html += "<a href=" + path + ">" + file.Name() + "</a><br>"
		}
	}
	html += "</body></html>"
	return html
}

func walkAddWatch(path string, info os.FileInfo, err error) error {
	if info.IsDir() {
		return addWatch(watches, path)
	}
	return nil
}

func addWatch(watches map[inotify.Wd]string, path string) error {
	wd, err := inot.AddWatch(path, inotify.IN_CREATE|inotify.IN_ATTRIB|inotify.IN_DELETE /*| inotify.IN_MODIFY*/ |inotify.IN_MOVED_TO|inotify.IN_MOVED_FROM|inotify.IN_ONLYDIR)
	if err != nil {
		return err
	}
	watches[wd] = path
	return nil
}
