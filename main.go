package main

import (
	"bufio"
	"flag"
	"fmt"
	"html/template"
	"htmldir/recursivedirwatch"
	"io/ioutil"
	"math"
	"net/url"
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
const kb = 1024
const mb = 1024 * 1024
const gb = 1024 * 1024 * 1024
const tb = 1024 * 1024 * 1024 * 1024

/*const kb uint = 1024
const mb uint = 1024*kb
const gb uint = 1024*mb
const tb uint = 1024*gb*/

func main() {
	if len(os.Args) != 2 && len(os.Args) != 3 && len(os.Args) != 4 {
		fmt.Println("Usage:", os.Args[0], "[-all] [-exit] basedirectory")
		return
	}
	var basedir string = os.Args[len(os.Args)-1]
	fmt.Println("Program is starting. Directory:", basedir)
	// basedirectory must be absolute?
	var regenall *bool = flag.Bool("all", false, "Regenerate all files before setting watches")
	var exit *bool = flag.Bool("exit", false, "Exit after regenerating all files")
	flag.Parse()
	fmt.Println(basedir)
	fmt.Println(*regenall)
	fmt.Println(*exit)

	// Remove trailing slash from dirname
	if len(basedir) > 1 && basedir[len(basedir)-1] == '/' {
		basedir = basedir[:len(basedir)-1]
	}
	fmt.Println(basedir)

	//var err error = nil // This would get compiled out right becauase already inititalised right?
	//var direvent recursivedirwatch.DirEvent
	//var files []os.FileInfo

	var tmpl *template.Template = template.Must(template.New("dirtemplate.html").ParseFiles("dirtemplate.html"))

	var ch chan recursivedirwatch.Event = make(chan recursivedirwatch.Event, 5) // Uber's go style guide says don't use buffered channels but I don't understand why
	go recursivedirwatch.Watch(basedir, ch, *regenall, *exit)
	for event := range ch {
		if event.Name == nil || *event.Name != outputfilename {
			fmt.Println("Making HTML for", event.Dirpath)
			var files, err = ioutil.ReadDir(event.Dirpath)
			if err != nil {
				fmt.Println(err)
			} else {
				var dirname, err = makeRelative(basedir, event.Dirpath)
				var dir templatedir = buildTemplateInputs(dirname, files)
				err = writeTemplate(event.Dirpath+"/"+outputfilename, tmpl, dir)
				if err != nil {
					fmt.Println(err)
				}
			}
		}
	}
}

// relative paths require that the uri ends in / for the browser to process them properly
// so what Apache does is redirect you to the uri ending in / if you are looking for a directory
// Or could make all links relative to base

func buildTemplateInputs(directory string, files []os.FileInfo) templatedir {
	//fmt.Println(directory)
	//fmt.Println(stringUpToLast(directory, '/'))
	var filetype string
	var templatedirv templatedir = templatedir{directory, make([]templatefile, 0, len(files))}
	var filesize string
	if directory != "" {
		templatedirv.Files = append(templatedirv.Files, templatefile{"DIR", "../", parentdir(directory), "", ""})
	}	
	for _, file := range files {
		var filename string = file.Name()
		if file.Mode()&os.ModeSymlink == 0 && filename != outputfilename && isReadable(directory, file) {
			// actually I probably do want to include symlinks
			if file.IsDir() {
				filetype = "DIR"
				filesize = ""
				filename += "/"
			} else {
				filetype = "FILE"
				//filesize = filesizestr(uint(math.Ceil(float64(file.Size()) / 1024))) // num kilobytes
				filesize = filesizestr(file.Size())
				// how do I know file.Size() / 1024 fits in float64
				// fmt.Println(filesizenum)
			}
			var lastmodified string = file.ModTime().Format("02-Jan-2006  15:04:05 MST")
			var link string
			if directory == "" {
				link = "/" + filename
			} else {
				link = "/" + directory + "/" + filename
			}
			//fmt.Println(url.PathEscape(link))

			urlurl, err := url.Parse(link)
			if err != nil {
				panic("boom")
			}
			//urlurl.Path += link
			//fmt.Printf("Encoded URL is %q\n", urlurl.String())

			templatedirv.Files = append(templatedirv.Files, templatefile{filetype, filename, urlurl.String(), filesize, lastmodified})
		}
	}
	return templatedirv
}

/*
 Find a better way of doing this
*/
func makeRelative(basedir string, dirpath string) (string, error) {
	// if basedir is prefix of dirpath, return dirpath - basedir
	// return a slice of string following basedir prefix
	// splitafter?
	var relative []string = strings.Split(dirpath, basedir+"/")
	if len(relative) == 1 {
		return "", nil
	} else {
		fmt.Println(relative)
		return relative[1], nil
	}
}

/*
 filesizenum is number of bytes
*/
func filesizestr(filesizenum int64) string {
	var filesize string
	//uint(math.Ceil(float64(file.Size()) / 1024)))
	// how know fits in float64
	switch {
	case filesizenum < kb:
		filesize = fmt.Sprintf("%d", filesizenum)
	case filesizenum < mb:
		filesize = fmt.Sprintf("%vKB", math.Ceil(float64(filesizenum)/kb))
	case filesizenum < gb:
		filesize = fmt.Sprintf("%vMB", math.Ceil(float64(filesizenum)/mb))
	case filesizenum < tb:
		filesize = fmt.Sprintf("%.1fGB", float64(filesizenum)/gb)
	default:
		filesize = fmt.Sprintf("%.1fTB", float64(filesizenum)/tb)
	}
	return filesize
	// test the sizes with big fake inputs
	// the numbers printed seem to be a big wrong eg 16MB when computer says 17.2
	// I changed it a bit since then, try again
}

func writeTemplate(location string, tmpl *template.Template, dir templatedir) error {
	var file, err = os.Create(location) // why didn't this cause inotify infinite loop when modidy was accepted
	if err != nil {
		return err
	}
	defer file.Close()
	var filewriter = bufio.NewWriter(file)
	err = tmpl.Execute(filewriter, dir)
	filewriter.Flush() //err
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

// Return string up to and including last instance of character 'end' in str
// If 'end' never appears, return new string with end as only character
func stringUpToLast(str string, end byte) string {
	for i := len(str) - 1; i > 0; i-- {
		if str[i] == end {
			return str[:i+1]
		}
	}
	return string(end)
}

// I feel like there's a better way of doing this
func parentdir(directory string) string {
	var parent = stringUpToLast(directory, '/')
	if parent != "/" {
		return "/" + parent
	}
	return parent
}
