/*
Generate HTML directory listings at .directory.html, recursively within basedir and its subdirectories
basedirectory must be absolute
// relative paths require that the uri for a directory welcome page ends in / for the browser to process them properly
// so what Apache does is redirect you to the uri ending in / if you are looking for a directory
// This program makes all links relative to basedirectory (with preceding /)
*/
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
	"path/filepath"
	"strings"
)

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
	// Process arguments
	if len(os.Args) != 2 && len(os.Args) != 3 && len(os.Args) != 4 {
		fmt.Println("Usage:", os.Args[0], "[-all] [-exit] basedirectory")
		return
	}
	var basedir string = os.Args[len(os.Args)-1]
	//fmt.Println(basedir)
	var regenall *bool = flag.Bool("all", false, "Regenerate all files before setting watches")
	var exit *bool = flag.Bool("exit", false, "Exit after regenerating all files")
	flag.Parse()
	//fmt.Println(*regenall, *exit)

	// Remove trailing slash from dirname
	if len(basedir) > 1 && basedir[len(basedir)-1] == '/' {
		basedir = basedir[:len(basedir)-1]
	}
	fmt.Println("Directory:", basedir)
	//-------------------------------------------------------------------------

	var err error // = nil // This would get compiled out right becauase already inititalised right?
	//var direvent recursivedirwatch.DirEvent
	//var files []os.FileInfo

	var tmpl *template.Template = template.Must(template.New("dirtemplate.html").ParseFiles("dirtemplate.html"))
	if *regenall && *exit {
		err = filepath.Walk(basedir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				makeHTML(path, basedir, tmpl)
			}
			return nil
		})
		if err != nil {
			fmt.Println(err)
		}
	} else {
		var ch chan recursivedirwatch.Event = make(chan recursivedirwatch.Event, 5) // Uber's go style guide says don't use buffered channels but I don't understand why
		go recursivedirwatch.Watch(basedir, ch, *regenall)
		for event := range ch {
			//recursivedirwatch.PrintEvent(event)
			if event.Name == nil || *event.Name != outputfilename {
				err = makeHTML(event.Dirpath, basedir, tmpl)
				if err != nil {
					fmt.Println(err)
				}
			}
		}
	}
}

func makeHTML(dirpath string, basedir string, tmpl *template.Template) error {
	fmt.Println("Making HTML for", dirpath)
	var files, err = ioutil.ReadDir(dirpath) // uses lstat
	if err != nil {
		return err
	}
	relpath := makeRelative(basedir, dirpath)
	dir, err := buildTemplateInputs(dirpath, relpath, files)
	if err != nil {
		return err
	}
	err = writeTemplate(dirpath+"/"+outputfilename, tmpl, dir)
	return err
}

func buildTemplateInputs(path string, relativepath string, files []os.FileInfo) (templatedir, error) {
	//fmt.Println(directory)
	//fmt.Println(stringUpToLast(directory, '/'))
	var filetype string
	var templatedirv templatedir = templatedir{relativepath, make([]templatefile, 0, len(files))}
	var filesize string
	if relativepath != "" {
		// add parent directory
		var parentpath string = parentdir(path)
		parent, err := os.Lstat(parentpath)
		if err != nil {
			return templatedir{}, err
			// what does templatedir{} actually return. Answer: all struct fields zero initialised
		}
		templatedirv.Files = append(templatedirv.Files, templatefile{"DIR", "../", "/" + parentdir(relativepath), "", parent.ModTime().Format("02-Jan-2006  15:04:05 MST")})
	}
	for _, file := range files {
		var filename string = file.Name()
		if file.Mode()&os.ModeSymlink == 0 && filename != outputfilename && isReadable(relativepath, file) {
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
			if relativepath == "" {
				link = "/" + filename
			} else {
				link = "/" + relativepath + "/" + filename
			}
			//fmt.Println(url.PathEscape(link)) // This encodes slashes... weird

			urlurl, err := url.Parse(link)
			if err != nil {
				return templatedir{}, err
			}

			templatedirv.Files = append(templatedirv.Files, templatefile{filetype, filename, urlurl.String(), filesize, lastmodified})
		}
	}
	return templatedirv, nil
}

/*
 Find a better way of doing this
*/
func makeRelative(basedir string, dirpath string) string {
	// if basedir is prefix of dirpath, return dirpath - basedir
	// return a slice of string following basedir prefix
	// splitafter?
	var relative []string = strings.Split(dirpath, basedir+"/")
	if len(relative) < 2 {
		return ""
	} else {
		return relative[1]
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
	// I think it is divide by 1024 vs divide by 1000
}

func writeTemplate(location string, tmpl *template.Template, dir templatedir) error {
	var file, err = os.Create(location) // why didn't this cause inotify infinite loop when modidy was accepted
	if err != nil {
		return err
	}
	defer file.Close()
	var filewriter = bufio.NewWriter(file)
	err = tmpl.Execute(filewriter, dir)
	if err != nil {
		return err
	}
	err = filewriter.Flush()
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

// Return string up to and including last instance of character 'end' in str.
// If 'end' never appears, return new string with end as only character
// func stringUpToLast {}

// stringUpToLast
func parentdir(directory string) string {
	for i := len(directory) - 1; i > 0; i-- {
		if directory[i] == '/' {
			return directory[:i+1]
		}
	}
	return ""

	/*
		var parent = stringUpToLast(directory, '/')
		if parent != "/" {
			return "/" + parent
		}
		return parent
	*/
}

// maybe use a closure so don't have to make basedir and tmpl global?
/*
func walkMakeHTML(path string, info os.FileInfo, err error) error {
	if err != nil {
		//fmt.Println(err)
		return err
	}
	if info.IsDir() {
		makeHTML(path)
	}
	return nil
}
*/
