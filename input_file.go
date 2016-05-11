package main

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"os"
	"strconv"
	"time"
)

// FileInput can read requests generated by FileOutput
type FileInput struct {
	data        chan []byte
	path        string
	file        *os.File
	speedFactor float64
}

// NewFileInput constructor for FileInput. Accepts file path as argument.
func NewFileInput(path string) (i *FileInput) {
	i = new(FileInput)
	i.data = make(chan []byte)
	i.path = path
	i.speedFactor = 1
	i.init(path)

	go i.emit()

	return
}

func (i *FileInput) init(path string) {
	file, err := os.Open(path)

	if err != nil {
		log.Fatal(i, "Cannot open file %q. Error: %s", path, err)
	}

	i.file = file
}

func (i *FileInput) Read(data []byte) (int, error) {
	buf := <-i.data
	copy(data, buf)

	return len(buf), nil
}

func (i *FileInput) String() string {
	return "File input: " + i.path
}

func (i *FileInput) emit() {
	var lastTime int64

	payloadSeparatorAsBytes := []byte(payloadSeparator)
	reader := bufio.NewReader(i.file)
	var buffer bytes.Buffer

	for {
		line, err := reader.ReadBytes('\n')

		if err != nil {
			if err != io.EOF {
				log.Fatal(err)
			}
			break

		}

		if bytes.Equal(payloadSeparatorAsBytes[1:], line) {
			asBytes := buffer.Bytes()[:buffer.Len()-1]
			buffer.Reset()

			meta := payloadMeta(asBytes)

			if len(meta) > 2 && meta[0][0] == RequestPayload {
				ts, _ := strconv.ParseInt(string(meta[2]), 10, 64)

				if lastTime != 0 {
					timeDiff := ts - lastTime

					if i.speedFactor != 1 {
						timeDiff = int64(float64(timeDiff) / i.speedFactor)
					}

					time.Sleep(time.Duration(timeDiff))
				}

				lastTime = ts
			}

			// Bytes() returns only pointer, so to remove data-race copy the data to an array
			newBuf := make([]byte, len(asBytes))
			copy(newBuf, asBytes)

			i.data <- newBuf
		} else {
			buffer.Write(line)
		}

	}

	log.Printf("FileInput: end of file '%s'\n", i.path)
}