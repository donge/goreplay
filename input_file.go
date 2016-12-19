package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type fileInputReader struct {
	reader    *bufio.Reader
	data      []byte
	file      *os.File
	timestamp int64
}

func (f *fileInputReader) parseNext() error {
	payloadSeparatorAsBytes := []byte(payloadSeparator)
	var buffer bytes.Buffer

	for {
		line, err := f.reader.ReadBytes('\n')

		if err != nil {
			if err != io.EOF {
				log.Println(err)
				return err
			}

			if err == io.EOF {
				f.file.Close()
				f.file = nil
				return err
			}
		}

		if bytes.Equal(payloadSeparatorAsBytes[1:], line) {
			asBytes := buffer.Bytes()
			meta := payloadMeta(asBytes)

			f.timestamp, _ = strconv.ParseInt(string(meta[2]), 10, 64)
			f.data = asBytes[:len(asBytes)-1]

			return nil
		}

		buffer.Write(line)
	}

	return nil
}

func (f *fileInputReader) ReadPayload() []byte {
	defer f.parseNext()

	return f.data
}
func (f *fileInputReader) Close() error {
	if f.file != nil {
		f.file.Close()
	}

	return nil
}

func NewFileInputReader(path string) *fileInputReader {
	file, err := os.Open(path)

	if err != nil {
		log.Println(err)
		return nil
	}

	r := &fileInputReader{file: file}
	if strings.HasSuffix(path, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			log.Println(err)
			return nil
		}
		r.reader = bufio.NewReader(gzReader)
	} else {
		r.reader = bufio.NewReader(file)
	}

	r.parseNext()

	return r
}

// FileInput can read requests generated by FileOutput
type FileInput struct {
	mu          sync.Mutex
	data        chan []byte
	exit        chan bool
	path        string
	readers     []*fileInputReader
	speedFactor float64
	loop        bool
}

// NewFileInput constructor for FileInput. Accepts file path as argument.
func NewFileInput(path string, loop bool) (i *FileInput) {
	i = new(FileInput)
	i.data = make(chan []byte, 1000)
	i.exit = make(chan bool, 1)
	i.path = path
	i.speedFactor = 1
	i.loop = loop

	if err := i.init(); err != nil {
		return
	}

	go i.emit()

	return
}

type NextFileNotFound struct{}

func (_ *NextFileNotFound) Error() string {
	return "There is no new files"
}

func (i *FileInput) init() (err error) {
	defer i.mu.Unlock()
	i.mu.Lock()

	var matches []string

	if matches, err = filepath.Glob(i.path); err != nil {
		log.Println("Wrong file pattern", i.path, err)
		return
	}

	if len(matches) == 0 {
		log.Println("No files match pattern: ", i.path)
		return errors.New("No matching files")
	}

	i.readers = make([]*fileInputReader, len(matches))

	for idx, p := range matches {
		i.readers[idx] = NewFileInputReader(p)
	}

	return nil
}

func (i *FileInput) Read(data []byte) (int, error) {
	buf := <-i.data
	copy(data, buf)

	return len(buf), nil
}

func (i *FileInput) String() string {
	return "File input: " + i.path
}

// Find reader with smallest timestamp e.g next payload in row
func (i *FileInput) nextReader() (next *fileInputReader) {
	for _, r := range i.readers {
		if r == nil || r.file == nil {
			continue
		}

		if next == nil || r.timestamp < next.timestamp {
			next = r
			continue
		}
	}

	return
}

func (i *FileInput) emit() {
	var lastTime int64 = -1

	for {
		select {
		case <-i.exit:
			return
		default:
		}

		reader := i.nextReader()

		if reader == nil {
			if i.loop {
				i.init()
				lastTime = -1
				continue
			} else {
				break
			}
		}

		if lastTime != -1 {
			diff := reader.timestamp - lastTime
			lastTime = reader.timestamp

			if i.speedFactor != 1 {
				diff = int64(float64(diff) / i.speedFactor)
			}

			time.Sleep(time.Duration(diff))
		} else {
			lastTime = reader.timestamp
		}

		i.data <- reader.ReadPayload()
	}

	log.Printf("FileInput: end of file '%s'\n", i.path)
}

func (i *FileInput) Close() error {
	defer i.mu.Unlock()
	i.mu.Lock()

	i.exit <- true

	for _, r := range i.readers {
		r.Close()
	}

	return nil
}