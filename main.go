package main

import (
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"code.cloudfoundry.org/bytefmt"
	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

type tFileData struct {
	path     string
	size     int64
	hash     []byte
	modified int64
}

type tStats struct {
	seconds       uint64
	count         uint32
	size          uint64
	duplicates    uint32
	duplicateSize uint64
	complted      bool
}

var (
	fileChannel     chan tFileData
	checksumChannel chan tFileData
	duplicates      map[string][]tFileData
	scanDir         string
	targetDir       string
	stats           tStats
	formatter       *message.Printer
)

func panicErr(err error) {
	if err != nil {
		log.Panicln(err)
	}
}

func walk(path string, info os.FileInfo, err error) error {
	if err != nil {
		// TODO: log err to a file
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if info.IsDir() {
		return nil
	}
	size := info.Size()
	if size > 0 {
		fileChannel <- tFileData{path, size, nil, info.ModTime().UnixNano()}
	}
	return nil
}

func checksum(file string) ([]byte, int64) {
	f, err := os.Open(file)
	panicErr(err)
	defer f.Close()
	h := crc32.New(crc32.IEEETable)
	buf := make([]byte, 2*1024*1024)
	size, err := io.CopyBuffer(h, f, buf)
	if err != nil {
		log.Panicln(err)
	}
	return h.Sum(nil), size
}

func formatPercent() string {
	if stats.size < 1 {
		return "-"
	}
	percent := float64(stats.duplicateSize) / float64(stats.size) * 100
	var color = "green"
	if percent > 15 {
		color = "red"
	} else if percent > 5 {
		color = "yellow"
	}
	percentStr := fmt.Sprintf("[%s]%.2f[%s]", color, percent, color)
	return percentStr
}

func listDuplicates() []string {
	result := make([]string, 0)
	for _, list := range duplicates {
		if len(list) < 2 {
			continue
		}
		for _, dup := range list[1:] {
			result = append(result, dup.path)
		}
	}
	return result
}

func deleteDuplicates(app *tview.Application) {
	// TODO: show modal to confirm
	count := 0
	list := listDuplicates()
	for _, path := range list {
		err := os.Remove(path)
		panicErr(err)
		count++
	}
	app.Stop()
	log.Printf("Deleted %d duplicate file(s)", count)
}

func ensureTargetDir() string {
	err := os.MkdirAll(targetDir, os.ModePerm)
	panicErr(err)
	return targetDir
}

func moveDuplicates(app *tview.Application) {
	ensureTargetDir()
	count := 0
	list := listDuplicates()
	for _, path := range list {
		err := os.Rename(path, filepath.Join(targetDir, filepath.Base(path)))
		panicErr(err)
		count++
	}
	app.Stop()
	log.Printf("Moved %d duplicate file(s) to: %s", count, targetDir)
}

func exportDuplicates(app *tview.Application) {
	// TODO: show modal to enter export file name
	path := filepath.Join(ensureTargetDir(), "duplicates.txt")
	file, err := os.Create(path)
	panicErr(err)
	defer file.Close()
	count := 0
	list := listDuplicates()
	for _, path := range list {
		_, err := file.WriteString(path)
		panicErr(err)
		file.WriteString("\n")
		count++
	}
	app.Stop()
	log.Printf("Exported %d duplicate file(s) to: %s", count, path)
}

func setupHotkeys(app *tview.Application) {
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyESC {
			app.Stop()
		} else if event.Key() == tcell.KeyCtrlE {
			exportDuplicates(app)
		} else if event.Key() == tcell.KeyCtrlM {
			moveDuplicates(app)
		} else if event.Key() == tcell.KeyCtrlUnderscore {
			deleteDuplicates(app)
		}
		return event
	})
}
func newTextView(title, text string) *tview.TextView {
	tv := tview.NewTextView().SetText(text).SetScrollable(false).SetTextAlign(tview.AlignLeft)
	tv.SetBorder(true).SetTitle(title).SetTitleAlign(tview.AlignLeft)
	return tv
}

func setupGui() (*tview.Application, *tview.Flex, *tview.TextView, *tview.List) {
	app := tview.NewApplication()
	path := newTextView("Path", scanDir)
	left := newTextView("Stats", "").SetDynamicColors(true)
	right := tview.NewList()
	right.SetBorder(true).SetTitle("Duplicates").SetTitleAlign(tview.AlignLeft)
	contextBox := tview.NewFlex().
		AddItem(left, 0, 1, false).
		AddItem(right, 0, 3, true)

	help := newTextView("Help", "Ctrl+e: Export\t Ctrl+m: Move\t Ctrl+_: Delete\t Ctrl+o: Open selected item")
	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(path, 3, 1, false).
		AddItem(contextBox, 0, 1, true).
		AddItem(help, 3, 1, false)

	return app, flex, left, right
}

func scan() {
	err := filepath.Walk(scanDir, walk)
	panicErr(err)
	stats.complted = true
}

func calculateChecksum() {
	for data := range fileChannel {
		data.hash, _ = checksum(data.path)
		checksumChannel <- data
	}
}

func findDuplicates(right *tview.List) {
	for d := range checksumChannel {
		stats.count++
		stats.size += uint64(d.size)
		hash := fmt.Sprintf("%x", d.hash)
		list, exist := duplicates[hash]
		if exist {
			list = append(list, d)
			// keep the oldes file always as head
			sort.Slice(list, func(i, j int) bool {
				return list[i].modified < list[j].modified
			})
			stats.duplicates++
			stats.duplicateSize += uint64(d.size)
			dupFiles := list[1].path
			if len(list[1:]) > 1 {
				dupFiles += formatter.Sprintf(" (+%d more)", len(list[1:])-1)
			}
			currentIndex := -1
			for i := 0; i < right.GetItemCount(); i++ {
				path, _ := right.GetItemText(i)
				if list[0].path == path {
					currentIndex = i
					break
				}
			}
			if currentIndex == -1 {
				right.AddItem(list[0].path, dupFiles, rune(stats.duplicates+32), nil)
			} else {
				right.SetItemText(currentIndex, list[0].path, dupFiles)
			}
		} else {
			list = make([]tFileData, 0)
			list = append(list, d)
		}
		duplicates[hash] = list
	}
}

func updateStats(left *tview.TextView) {
	for range time.Tick(time.Second * 1) {
		stats.seconds++
		var done string
		if done = "[red]No[red]"; stats.complted {
			done = "[green]Yes[green]"
		}
		percent := formatPercent()
		speed := float64(stats.size) / float64(stats.seconds)
		left.SetText(
			formatter.Sprintf(
				"Elapsed: %d seconds\nScanned: %d\nSize: %s\nRead Speed: %s\nDuplicates: %d\nDuplicate Size: %s\nDuplicate Percent: %s\nFinished: %s",
				stats.seconds,
				stats.count, bytefmt.ByteSize(stats.size), bytefmt.ByteSize(uint64(speed)),
				stats.duplicates, bytefmt.ByteSize(stats.duplicateSize),
				percent,
				done))
		//right.SetText(strconv.FormatInt(counter, 10))
		if stats.complted {
			break
		}
	}
}

func main() {
	fileChannel = make(chan tFileData, 200)
	defer close(fileChannel)
	checksumChannel = make(chan tFileData, 100)
	defer close(checksumChannel)

	duplicates = make(map[string][]tFileData)
	stats = tStats{0, 0, 0, 0, 0, false}
	formatter = message.NewPrinter(language.English)
	if len(os.Args) > 2 {
		scanDir = os.Args[1]
		targetDir = os.Args[2]
	} else {
		if len(os.Args) == 2 {
			scanDir = os.Args[1]
		} else {
			scanDir = "."
		}
		targetDir = filepath.Join(scanDir, ".dup-fu")
	}

	app, flex, left, right := setupGui()
	setupHotkeys(app)
	left.SetChangedFunc(func() {
		app.Draw()
	})

	go updateStats(left)
	go scan()
	go calculateChecksum()
	go calculateChecksum()
	go findDuplicates(right)

	err := app.SetRoot(flex, true).SetFocus(flex).Run()
	panicErr(err)
}
