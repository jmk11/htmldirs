package main

import (
	"bufio"
	"fmt"
	"html/template"
	"htmldir/recursivedirwatch"
	"io/ioutil"
	"math"
	"os"
	"strings"
)

// name
type templatedir struct {
	Dirname string
	Files   []templatefile
}

type templatefile struct {
	Filetype     string
	Name         string
	Link         string
	Size         string
	LastModified string
}

const outputfilename = ".directory.html"

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage:", os.Args[0], "basedirectory")
		return
	}
	var basedir string = os.Args[1]
	fmt.Println("Program is starting. Directory:", basedir)
	// basedirectory must be absolute?

	//var err error = nil // This would get compiled out right becauase already inititalised right?
	//var direvent recursivedirwatch.DirEvent
	//var files []os.FileInfo

	var tmpl = template.Must(template.New("dirtemplate.html").ParseFiles("dirtemplate.html"))
	ch := make(chan recursivedirwatch.Event, 5) // Uber's go style guide says don't use buffered channels but I don't understand why
	go recursivedirwatch.Watch(basedir, ch)
	var dir templatedir
	for event := range ch {
		if event.Name == nil || *event.Name != outputfilename {
			fmt.Println("Making HTML for", event.Dirname)
			files, err := ioutil.ReadDir(event.Dirname)
			if err != nil {
				fmt.Println(err)
			} else {
				dirname, err := makeRelative(basedir, event.Dirname)
				dir = buildTemplateInputs(dirname, files)
				err = writeTemplate(event.Dirname+"/"+outputfilename, tmpl, dir)
				if err != nil {
					fmt.Println(err)
				}
				/*err = writeHTML(direvent.Dirname, files)
				if err != nil {
					fmt.Println(err)
				}*/
				/*var html string = makeHTML(direvent.Dirname, files)
				//fmt.Println(html)
				err = writeFile(direvent.Dirname+"/"+outputfilename, html)
				if err != nil {
					fmt.Println(err)
				}*/
			}
		}
	}
}

// return slice starting at character after last delim
// if last character is delim or delim isn't in string at all, return empty string
/*
func stringAfterLast(src string, delim byte) string {
	for i := len(src) - 2; i >= 0; i-- {
		if src[i] == delim {
			return src[i+1:]
		}
	}
	return ""
}
*/

// relative paths require that the uri ends in / for the browser to process them properly
// so what Apache does is redirect you to the uri ending in / if you are looking for a directory
// Or could make all links relative to base

// html: doesn't display properly when 

func buildTemplateInputs(directory string, files []os.FileInfo) templatedir {
	var filetype string
	templatedirv := templatedir{directory, make([]templatefile, 0, len(files))}
	// could make templatefiles.files an array of same size as files slice because won't be bigger
	// and then create a slice of the part of that that is actually used before passing to template
	// should be faster than making a new slice over and over again
	// you don't have to do that in go - just provide capacity for slice and it will do that work for you
	var filesize string
	for _, file := range files {
		filename := file.Name()
		if file.Mode()&os.ModeSymlink == 0 && filename != outputfilename && isReadable(directory, file) {
			// actually I probably do want to include symlinks
			if file.IsDir() {
				filetype = "DIR"
				filesize = ""
			} else {
				filetype = "FILE"
				filesize = filesizestr(uint(math.Ceil(float64(file.Size()) / 1024))) // num kilobytes
				// how do I know file.Size() / 1024 fits in float64
				// fmt.Println(filesizenum)
			}
			lastmodified := file.ModTime().Format("02-Jan-2006	15:04:05 MST")
			var link string
			if directory == "" {
				link = filename
			} else {
				link = "/" + directory + "/" + filename
			}
			templatedirv.Files = append(templatedirv.Files, templatefile{filetype, filename, link, filesize, lastmodified})
		}
	}
	return templatedirv
}

/*
* 
*/
func makeRelative(basedir string, dirpath string) (string, error) {
	// if basedir is prefix of dirpath, return dirpath - basedir
	relative := strings.Split(dirpath, basedir + "/")
	if len(relative) == 1 {
		return "", nil
	} else {
		fmt.Println(relative)
		return relative[1], nil
	}
}

func filesizestr(filesizenum uint) string {
	var filesize string
	switch {
	case filesizenum < 1024:
		filesize = fmt.Sprintf("%dKB", filesizenum)
	case filesizenum < 1024*1024:
		filesize = fmt.Sprintf("%dMB", filesizenum/1024)
	case filesizenum < 1024*1024*1024:
		filesize = fmt.Sprintf("%dGB", filesizenum/(1024*1024))
	default:
		filesize = fmt.Sprintf("%dTB", filesizenum/(1024*1024*1024))
	}
	return filesize
	// test the sizes with big fake inputs
	// the numbers printed seem to be a big wrong eg 16MB when computer says 17.2
}

func writeTemplate(location string, tmpl *template.Template, dir templatedir) error {
	var file, err = os.Create(location) // why doesn't this cause inotify infinite loop
	if err != nil {
		return err
	}
	defer file.Close()
	var filewriter = bufio.NewWriter(file)
	err = tmpl.Execute(filewriter, dir)
	filewriter.Flush()
	return err
}

func isReadable(dir string, file os.FileInfo) bool {
	// check that files are readable before adding
	// file.Mode()&os.ModePerm
	// will require checking if is owner and owner permissions,
	// but also checking all groups the user is in and if any of them have access
	// Linux has access() and euidaccess()
	return true
}

/*
func writeHTML(directory string, files []os.FileInfo) error {

	var html string = makeHTML(directory, files)
	fmt.Println(html)
	err := writeFile(directory+"/"+outputfile, html)
	return err
}*/

// func writeHTML(directory string, files []os.FileInfo, template *template.Template) error {
// 	var filetype string
// 	templatedirv := templatedir{directory, make([]templatefile)}
// 	// could make templatefiles.files an array of same size as files slice because won't be bigger
// 	// and then create a slice of the part of that that is actually used before passing to template
// 	// should be faster than making a new slice over and over again
// 	for _, file := range files {
// 		if file.Mode()&os.ModeSymlink == 0 && file.Name() != outputfilename && isReadable(directory, file) {
// 			// actually I probably do want to include symlinks
// 			if file.IsDir() {
// 				filetype = "DIR"
// 			} else {
// 				filetype = "FILE"
// 			}
// 			templatedirv.files = append(templatedirv.files, templatefile{filetype, file.Name(), file.Size(), "Some time"})
// 		}
// 	}
// 	filewriter := bufio.NewWriter()
// 	template.Execute(filewriter, templatedirv)
// }

// second loop through files list - combine this and watchNewDirs?
// use html/templates
// why waste time writing to buffer just to write it to file
// func makeHTML(directory string, files []os.FileInfo) string {
// 	var filetype string
// 	templatedirv := templatedir{directory, make([]templatefile)}
// 	// could make templatefiles.files an array of same size as files slice because won't be bigger
// 	// and then create a slice of the part of that that is actually used before passing to template
// 	// should be faster than making a new slice over and over again
// 	for _, file := range files {
// 		if file.Mode()&os.ModeSymlink == 0 && file.Name() != outputfilename && isReadable(directory, file) {
// 			// actually I probably do want to include symlinks
// 			if file.IsDir() {
// 				filetype = "DIR"
// 			} else {
// 				filetype = "FILE"
// 			}
// 			templatedirv.files = append(templatedirv.files, templatefile{filetype, file.Name(), file.Size(), "Some time"})
// 		}
// 	}
// 	return html
// }

// func writeFile(location string, contents string) error {
// 	file, err := os.Create(location) // why doesn't this cause inotify infinite loop
// 	if err != nil {
// 		return err
// 	}
// 	_, err = file.WriteString(contents)
// 	if err != nil {
// 		return err
// 	}
// 	err = file.Close()
// 	return err
// 	/*if err != nil {
// 		return err
// 	}
// 	return nil*/
// }

/*
func makeHTML(directory string, files []os.FileInfo) string {
	var html string = "<html><head></head><body>\n"
	for _, file := range files {
		if file.Mode()&os.ModeSymlink == 0 && file.Name() != outputfilename && isReadable(directory, file) {
			var path string = directory + "/" + file.Name()
			html += "<a href=" + path + ">" + file.Name() + "</a><br>"
		}
	}
	html += "</body></html>"
	return html
}
*/
