package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"
	"github.com/mattn/go-shellwords"
	"github.com/peterh/liner"
	"github.com/studio-b12/gowebdav"
)

var invalidArg = errors.New("invalid argument")

var (
	cred       = flag.String("cred", os.Getenv("DAVC_CRED"), "credential for basic auth (user:password)")
	prompthere = flag.Bool("prompthere", false, "display location at prompt")
)

func fatalRequiredAuth(err error) {
	fmt.Fprintln(os.Stderr, os.Args[0]+":", err)
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, os.Args[0]+":", err)
	os.Exit(1)
}

var esc = strings.NewReplacer(
	`\`, `\\`,
	` `, `\ `,
)

func escape(s string) string {
	return esc.Replace(s)
}

func parseArgs(args []string) (opts map[string]bool, retargs []string) {
	opts = map[string]bool{}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			opts[arg[1:]] = true
		} else {
			retargs = append(retargs, arg)
		}
	}
	return
}

func handle(client *gowebdav.Client, cwd *string, args []string) error {
	lwd, err := os.Getwd()
	if err != nil {
		return err
	}

	switch args[0] {
	case "lpwd":
		if len(args) != 1 {
			return invalidArg
		}
		fmt.Println(lwd)
	case "pwd":
		if len(args) != 1 {
			return invalidArg
		}
		fmt.Println(*cwd)
	case "lcd":
		if len(args) != 2 {
			return invalidArg
		}
		p := args[1]
		if !filepath.IsAbs(p) {
			p = filepath.Join(lwd, p)
		}
		fi, err := os.Stat(p)
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return os.ErrNotExist
		}
		err = os.Chdir(filepath.Clean(p))
		if err != nil {
			return err
		}
	case "cd":
		if len(args) != 2 {
			return invalidArg
		}
		p := args[1]
		if !path.IsAbs(p) {
			p = path.Join(*cwd, p)
		}
		if !strings.HasSuffix(p, "/") {
			p += "/"
		}
		fi, err := client.Stat(p)
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return os.ErrNotExist
		}
		*cwd = path.Clean(p)
		if !strings.HasSuffix(*cwd, "/") {
			*cwd += "/"
		}
	case "lmkdir":
		if len(args) != 2 {
			return invalidArg
		}
		p := args[1]
		if !filepath.IsAbs(p) {
			p = filepath.Join(lwd, p)
		}
		err := os.MkdirAll(p, 0755)
		if err != nil {
			return err
		}
	case "mkdir":
		if len(args) != 2 {
			return invalidArg
		}
		p := args[1]
		if !path.IsAbs(p) {
			p = path.Join(*cwd, p)
		}
		err := client.MkdirAll(p, 0755)
		if err != nil {
			return err
		}
	case "lls":
		if len(args) != 1 {
			return invalidArg
		}
		f, err := os.Open(lwd)
		if err != nil {
			return nil
		}
		defer f.Close()
		fis, err := f.Readdir(0)
		if err != nil {
			return nil
		}
		for _, fi := range fis {
			if fi.IsDir() {
				fmt.Fprintln(color.Output, color.GreenString("%v", fi.Name()+"/"))
			} else {
				fmt.Fprintln(color.Output, fi.Name())
			}
		}
	case "ls":
		var target string
		var opts map[string]bool
		var jsonout bool

		opts, args = parseArgs(args)
		jsonout = opts["json"]
		if len(args) == 1 {
			target = *cwd
		} else if len(args) == 2 {
			target = args[1]
			if !path.IsAbs(target) {
				target = path.Join(*cwd, target)
			}
		} else {
			return invalidArg
		}
		target = path.Clean(target)
		fis, err := client.ReadDir(target)
		if err != nil {
			return err
		}
		if jsonout {
			type fit struct {
				Name    string    `json:"name"`
				Size    int64     `json:"size"`
				Mode    string    `json:"mode"`
				ModTime time.Time `json:"modtime"`
				IsDir   bool      `json:"isdir"`
			}
			ffs := make([]fit, len(fis))
			for i, fi := range fis {
				ffs[i].Name = fi.Name()
				ffs[i].Size = fi.Size()
				ffs[i].Mode = fi.Mode().String()
				ffs[i].ModTime = fi.ModTime()
				ffs[i].IsDir = fi.IsDir()
			}
			json.NewEncoder(color.Output).Encode(ffs)
		} else {
			for _, fi := range fis {
				if fi.IsDir() {
					fmt.Fprintln(color.Output, color.GreenString("%v", fi.Name()+"/"))
				} else {
					fmt.Fprintln(color.Output, fi.Name())
				}
			}
		}
	case "ll":
		if len(args) != 1 {
			return invalidArg
		}
		fis, err := client.ReadDir(*cwd)
		if err != nil {
			return err
		}
		for _, fi := range fis {
			if fi.IsDir() {
				fmt.Fprintln(color.Output,
					color.GreenString(runewidth.Truncate(fmt.Sprintf("%-20s", fi.Name()+"/"), 20, ""))+"\t"+
						fmt.Sprintf("%20d", fi.Size())+"\t"+
						fi.ModTime().String())
			} else {
				fmt.Fprintln(color.Output,
					runewidth.Truncate(fmt.Sprintf("%-20s", fi.Name()), 20, "")+"\t"+
						fmt.Sprintf("%20d", fi.Size())+"\t"+
						fi.ModTime().String())
			}
		}
	case "lrm":
		if len(args) != 2 {
			return invalidArg
		}
		p := args[1]
		if !filepath.IsAbs(p) {
			p = filepath.Join(lwd, p)
		}
		err := os.Remove(p)
		if err != nil {
			return err
		}
	case "rm":
		if len(args) != 2 {
			return invalidArg
		}
		p := args[1]
		if !path.IsAbs(p) {
			p = path.Join(*cwd, p)
		}
		err := client.Remove(p)
		if err != nil {
			return err
		}
	case "lrmdir":
		if len(args) != 2 {
			return invalidArg
		}
		p := args[1]
		if !filepath.IsAbs(p) {
			p = filepath.Join(lwd, p)
		}
		err := os.RemoveAll(p)
		if err != nil {
			return err
		}
	case "rmdir":
		if len(args) != 2 {
			return invalidArg
		}
		p := args[1]
		if !path.IsAbs(p) {
			p = path.Join(*cwd, p)
		}
		err := client.Remove(p)
		if err != nil {
			return err
		}
	case "put":
		if len(args) != 2 {
			return invalidArg
		}
		p := args[1]
		if !filepath.IsAbs(p) {
			p = filepath.Join(lwd, p)
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		_, file := filepath.Split(p)
		file = path.Join(*cwd, file)
		err = client.WriteStream(file, f, 0644)
		if err != nil {
			return err
		}
	case "get":
		if len(args) != 2 {
			return invalidArg
		}
		p := args[1]
		if !path.IsAbs(p) {
			p = path.Join(*cwd, p)
		}
		_, file := path.Split(p)
		strm, err := client.ReadStream(p)
		if err != nil {
			return err
		}
		defer strm.Close()
		f, err := os.Create(file)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(f, strm)
		if err == io.ErrUnexpectedEOF {
			return nil
		}
		return err
	case "cp":
		if len(args) != 3 {
			return invalidArg
		}
		src := args[1]
		if !path.IsAbs(src) {
			src = path.Join(*cwd, src)
		}
		dst := args[2]
		if !path.IsAbs(dst) {
			dst = path.Join(*cwd, dst)
		}
		err := client.Copy(src, dst, true)
		if err != nil {
			return err
		}
	case "mv":
		if len(args) != 3 {
			return invalidArg
		}
		src := args[1]
		if !path.IsAbs(src) {
			src = path.Join(*cwd, src)
		}
		dst := args[2]
		if !path.IsAbs(dst) {
			dst = path.Join(*cwd, dst)
		}
		err := client.Rename(src, dst, true)
		if err != nil {
			return err
		}
	case "cat":
		if len(args) != 2 {
			return invalidArg
		}
		p := args[1]
		if !path.IsAbs(p) {
			p = path.Join(*cwd, p)
		}
		strm, err := client.ReadStream(p)
		if err != nil {
			return err
		}
		defer strm.Close()
		_, err = io.Copy(os.Stdout, strm)
		if err == io.ErrUnexpectedEOF {
			err = nil
		}
		return err
	case "write":
		if len(args) != 2 {
			return invalidArg
		}
		p := args[1]
		if !path.IsAbs(p) {
			p = path.Join(*cwd, p)
		}
		buf := bufio.NewReader(os.Stdin)
		err = client.WriteStream(p, buf, 0644)
		if err != nil {
			return err
		}
		return err
	case "edit", "vim":
		if len(args) != 2 {
			return invalidArg
		}
		p := args[1]
		if !path.IsAbs(p) {
			p = path.Join(*cwd, p)
		}
		strm, err := client.ReadStream(p)
		if err != nil {
			return err
		}
		defer strm.Close()
		f, err := ioutil.TempFile("", "davc")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		_, err = io.Copy(f, strm)
		f.Close()
		if err != nil { //&& err != io.ErrUnexpectedEOF {
			return err
		}
		fi, err := os.Stat(f.Name())
		if err != nil {
			return err
		}
		editor := os.Getenv("EDITOR")
		if args[0] == "vim" {
			editor = "vim"
		}
		cmd := exec.Command(editor, f.Name())
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			return nil
		}
		f, err = os.Open(f.Name())
		if err != nil {
			return err
		}
		nfi, err := f.Stat()
		if err != nil {
			return err
		}
		if nfi.ModTime().Equal(fi.ModTime()) {
			return nil
		}
		err = client.WriteStream(p, f, 0644)
		if err != nil {
			return err
		}
	case "exit":
		os.Exit(0)
	default:
		return errors.New("unknown command")
	}
	return nil
}

var localCommands = []string{"lpwd", "lmkdir", "lrm", "lrmdir"}
var remoteCommands = []string{"cd", "pwd", "mkdir", "rm", "rmdir", "cat", "edit", "vim", "get", "cp", "mv"}
var allCommands = []string{}

func init() {
	allCommands = append(allCommands, localCommands...)
	allCommands = append(allCommands, remoteCommands...)
}

func isLocalCompletion(cmd string, narg int) (bool, bool) {
	if cmd == "put" {
		if narg == 2 {
			return false, false
		} else {
			return true, false
		}
	}
	for _, n := range []string{"put"} {
		if cmd == n {
			return true, false
		}
	}
	for _, n := range []string{"cat", "edit"} {
		if cmd == n {
			return false, false
		}
	}
	for _, n := range []string{"lcd", "lmkdir", "lrmdir"} {
		if cmd == n {
			return true, true
		}
	}
	for _, n := range []string{"cd", "mkdir", "rmdir"} {
		if cmd == n {
			return false, true
		}
	}
	for _, n := range localCommands {
		if cmd == n {
			return true, true
		}
	}
	return false, false
}

func complete(client *gowebdav.Client, cwd *string, l string) (c []string) {
	args, err := shellwords.Parse(string(l))
	if err != nil || len(args) == 0 {
		return allCommands
	}
	if len(args) == 1 && !strings.HasSuffix(l, " ") {
		for _, cc := range allCommands {
			if strings.HasPrefix(cc, l) {
				c = append(c, cc)
			}
		}
		return
	}
	ncomplete := len(args)
	if len(args) > 1 && !strings.HasSuffix(l, " ") {
		ncomplete++
	}
	var p string
	if local, listdir := isLocalCompletion(args[0], ncomplete); local {
		lwd, err := os.Getwd()
		if err != nil {
			return nil
		}
		if len(args) > 1 && !strings.HasSuffix(l, " ") {
			p = filepath.ToSlash(args[len(args)-1])
			slashed := strings.HasSuffix(p, "/")
			if !filepath.IsAbs(p) {
				p = filepath.Join(lwd, p)
				if slashed && !strings.HasSuffix(p, "/") {
					p += "/"
				}
			}
		} else {
			p = lwd + "/"
		}
		dir, file := filepath.Split(p)
		f, err := os.Open(dir)
		if err != nil {
			return nil
		}
		defer f.Close()
		fis, err := f.Readdir(0)
		if err != nil {
			return nil
		}
		for _, fi := range fis {
			if listdir && !fi.IsDir() {
				continue
			}
			if len(file) == 0 {
				c = append(c, l+escape(fi.Name()))
			} else {
				if strings.HasPrefix(fi.Name(), file) {
					c = append(c, l+escape(fi.Name()[len(file):]))
				}
			}
		}
	} else {
		if len(args) > 1 && !strings.HasSuffix(l, " ") {
			p = args[len(args)-1]
			slashed := strings.HasSuffix(p, "/")
			if !path.IsAbs(p) {
				p = path.Join(*cwd, p)
				if slashed && !strings.HasSuffix(p, "/") {
					p += "/"
				}
			}
		} else {
			p = *cwd
		}
		dir, file := path.Split(p)
		fis, err := client.ReadDir(dir)
		if err != nil {
			return nil
		}
		for _, fi := range fis {
			if listdir && !fi.IsDir() {
				continue
			}
			if len(file) == 0 {
				c = append(c, l+escape(fi.Name()))
			} else {
				if strings.HasPrefix(fi.Name(), file) {
					c = append(c, l+escape(fi.Name()[len(file):]))
				}
			}
		}
	}
	return
}

func main() {
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		return
	}
	user, password := "", ""
	if *cred != "" {
		token := strings.SplitN(*cred, ":", 2)
		if len(token) != 2 {
			flag.Usage()
			return
		}
		user, password = token[0], token[1]
	}

	line := liner.NewLiner()
	defer line.Close()

	line.SetCtrlCAborts(true)

	u, err := url.Parse(flag.Arg(0))
	if err != nil {
		fatal(err)
	}
	if u.Host == "" && u.Path != "" {
		u.Host, u.Path = u.Path, ""
	}

	switch u.Scheme {
	case "webdav", "http":
		u.Scheme = "http"
	case "webdavs", "https":
		u.Scheme = "https"
	default:
		u.Scheme = "https"
	}

	client := gowebdav.NewClient(u.Scheme+"://"+u.Host, user, password)
	err = client.Connect()
	if err != nil {
		ep, ok := err.(*os.PathError)
		if ok {
			err = ep.Err
		}
		switch err.Error() {
		case "200":
		case "401":
			if *cred != "" {
				fatal(err)
			}
			user, err = line.Prompt("User: ")
			if err != nil {
				fatalRequiredAuth(err)
			}
			password, err = line.PasswordPrompt("Password: ")
			if err != nil {
				fatalRequiredAuth(err)
			}
			client = gowebdav.NewClient(u.Scheme+"://"+u.Host, user, password)
			err = client.Connect()
			if err != nil {
				fatal(err)
			}
		default:
			fatal(err)
		}
	}

	cwd := u.Path
	if !strings.HasSuffix(cwd, "/") {
		cwd += "/"
	}
	if flag.NArg() == 1 {
		line := liner.NewLiner()
		defer line.Close()

		line.SetCompleter(func(l string) (c []string) {
			return complete(client, &cwd, l)
		})

		for {
			prompt := "> "
			if *prompthere {
				prompt = cwd + "> "
			}
			l, err := line.Prompt(prompt)
			if err != nil {
				break
			}
			args, err := shellwords.Parse(l)
			if err != nil {
				fmt.Fprintln(color.Output, color.RedString("%v", err.Error()))
				continue
			}
			if len(args) == 0 {
				continue
			}
			line.AppendHistory(l)
			err = handle(client, &cwd, args)
			if err != nil {
				fmt.Fprintln(color.Output, color.RedString("%v", err.Error()))
				continue
			}
		}
	} else {
		err = handle(client, &cwd, flag.Args()[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
}
