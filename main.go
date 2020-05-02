package main

import (
	"fmt"
	"htmldir/recursivedirwatch"
	"io/ioutil"
	"os"
)

const outputfilename = ".directory.html"

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage:", os.Args[0], "basedirectory")
		return
	}
	var basedir string = os.Args[1]
	fmt.Println("Program is starting. Directory:", basedir)

	//var err error = nil // This would get compiled out right becauase already inititalised right?
	//var direvent recursivedirwatch.DirEvent
	//var files []os.FileInfo

	ch := make(chan recursivedirwatch.DirEvent, 5)
	go recursivedirwatch.Dirwatch(basedir, ch)
	for direvent := range ch {
		if direvent.Name == nil || *direvent.Name != outputfilename {
			fmt.Println("Making HTML for", direvent.Dirname)
			files, err := ioutil.ReadDir(direvent.Dirname)
			if err != nil {
				fmt.Println(err)
			} else {
				var html string = makeHTML(direvent.Dirname, files)
				//fmt.Println(html)
				err = writeFile(direvent.Dirname+"/"+outputfilename, html)
				if err != nil {
					fmt.Println(err)
				}
			}
		}
	}
}

/*
func writeHTML(directory string, files []os.FileInfo) error {

	var html string = makeHTML(directory, files)
	fmt.Println(html)
	err := writeFile(directory+"/"+outputfile, html)
	return err
}*/

func writeFile(location string, contents string) error {
	file, err := os.Create(location) // why doesn't this cause inotify infinite loop
	if err != nil {
		return err
	}
	_, err = file.WriteString(contents)
	if err != nil {
		return err
	}
	err = file.Close()
	return err
	/*if err != nil {
		return err
	}
	return nil*/
}

// second loop through files list - combine this and watchNewDirs?
// use html/templates
func makeHTML(directory string, files []os.FileInfo) string {
	var html string = "<html><head></head><body>\n"
	for _, file := range files {
		if file.Mode()&os.ModeSymlink == 0 && file.Name() != outputfilename {
			var path string = directory + "/" + file.Name()
			html += "<a href=" + path + ">" + file.Name() + "</a><br>"
		}
	}
	html += "</body></html>"
	return html
}
