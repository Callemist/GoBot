package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"layeh.com/gopus"
)

func main() {
	token, err := readToken()
	if err != nil {
		log.Fatal(err)
	}

	gw, err := newGateway(token)
	if err != nil {
		log.Fatal(err)
	}

	voi := newVoice()

	gw.eventHandlers[messageCreateEvent] = func(data json.RawMessage) {
		var m message
		err := json.Unmarshal(data, &m)
		if err != nil {
			log.Println("error parsing json", err)
			return
		}

		if m.Content != "!s" && m.Content != "!e" {
			return
		}

		var cID string
		if m.Content == "!s" {
			cID = "327232994102214656"
		}

		if m.Content == "!e" {
			voi.speaking(false)
			cID = "220255406964998144"
		}

		const (
			channels  int = 2                   // 1 for mono, 2 for stereo
			frameRate int = 48000               // audio sampling rate
			frameSize int = 960                 // uint16 size of each audio frame
			maxBytes  int = (frameSize * 2) * 2 // max size of opus data
		)

		opusEncoder, err := gopus.NewEncoder(frameRate, channels, gopus.Audio)
		if err != nil {
			log.Printf("error creating encoder: %v\n", err)
			return
		}

		// ####### download video as webm #######

		connected, err := voi.establishConnection(cID, gw)
		if err != nil {
			log.Printf("error establishing voice connection: %v\n", err)
			return
		}

		err = <-connected
		if err != nil {
			log.Printf("error establishing voice connection: %v\n", err)
		}

		go func() {
			err := <-connected
			if err != nil {
				log.Printf("voice connection error: %v\n", err)
			}
		}()

		voi.speaking(true)

		yID := "8-kcWrCX9rA" // 1:58 long
		filename := yID + ".webm"

		cmd := exec.Command("youtube-dl", "-f 251", yID, "--id")
		err = cmd.Run()
		if err != nil {
			log.Printf("error downloading youtube video: %v\n", err)
			log.Println("trying again..")

			cmd := exec.Command("youtube-dl", "-f 251", yID, "--id")
			err = cmd.Run()

			if err != nil {
				log.Printf("failed to download youtube video on second attempt: %v\n", err)
				return
			}
		}

		//####### extract opus data with ffmpeg #######

		ffmpegCMD := exec.Command("ffmpeg", "-i", filename, "-f", "s16le", "-ar", strconv.Itoa(frameRate), "-ac", strconv.Itoa(channels), "pipe:1")
		ffmpegout, err := ffmpegCMD.StdoutPipe()
		if err != nil {
			log.Printf("stdoutPipe error: %v\n", err)
			return
		}

		ffmpegCMD.Start()
		if err != nil {
			log.Printf("error running ffmpeg: %v\n", err)
			return
		}

		ffmpegbuf := bufio.NewReaderSize(ffmpegout, 16384)

		for {
			// read data from ffmpeg stdout
			audiobuf := make([]int16, frameSize*channels)
			err = binary.Read(ffmpegbuf, binary.LittleEndian, &audiobuf)
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return
			}
			if err != nil {
				log.Printf("error reading from ffmpeg stdout: %v\n", err)
				return
			}

			// Send received PCM to the sendPCM channel

			opus, err := opusEncoder.Encode(audiobuf, frameSize, maxBytes)
			if err != nil {
				log.Printf("error encoding opus: %v\n", err)
				return
			}

			//log.Println("opus: ", len(opus))
			voi.sendOpusData(opus)
		}

		// ffmpegCMD.Wait()
		// if err != nil {
		// 	log.Printf("error waiting for ffmpeg: %v\n", err)
		// 	return
		// }

		//url := "https://discordapp.com/api/v6/channels/" + m.ChannelID + "/messages"
		// var jsonStr = []byte(`{"content":"Establishing voice", "tts": false}`)
		// req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))
		// req.Header.Set("Content-Type", "application/json")
		// req.Header.Set("Authorization", "Bot "+token)

		// if err != nil {
		// 	log.Println(err)
		// }

		// client := &http.Client{}
		// _, err = client.Do(req)

		// if err != nil {
		// 	log.Println(err)
		// }

	}

	go gw.open()

	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

func ytdl() {
	yID := "LDU_Txk06tM"
	cmd := exec.Command("youtube-dl", "-o", "-", "-f 251", yID)
	//cmd := exec.Command("youtube-dl", "-o", "-", "-f 251", yID, "|", "ffmpeg", "-i", "pipe:", "-f", "s16le", "-ar", "48000", "-ac", "2", "pipe:")

	p, _ := os.Getwd()
	f, err := os.Create(p + "/video.webm")

	if err != nil {
		log.Fatal(fmt.Errorf("error creating file: %v", err))
	}
	defer f.Close()
	//bw := bufio.NewWriter(f)

	r, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(fmt.Errorf("error getting StdoutPipe: %v", err))
	}

	err = cmd.Start()
	if err != nil {
		log.Fatal(fmt.Errorf("error starting command: %v", err))
	}

	var totalBytes int64
	buf := make([]byte, 0, 4096)
	for {
		// using buf[:cap(buf)] will make the slice use the whole underlying array of 4096 bytes
		n, err := r.Read(buf[:cap(buf)])

		// only use the read bytes of buf
		// since the slice is between 0 and n the cap of buf will not change
		buf = buf[:n]

		// read will not return an error if n > 0
		if n == 0 {
			if err == nil {
				// nothing happend
				continue
			}
			if err == io.EOF {
				break
			} else {
				log.Printf("read error: %v\n", err)
			}
		}
		_, err = f.Write(buf)
		if err != nil {
			log.Fatal(fmt.Errorf("error writing to file: %v", err))
		}

		totalBytes += int64(len(buf))
	}

	f.Sync()
	log.Println("total bytes", totalBytes)
}

func readToken() (string, error) {
	b, err := ioutil.ReadFile("go-bot-token.txt")
	if err != nil {
		return "", fmt.Errorf("error reading token file: %v", err)
	}
	return strings.TrimRight(string(b), "\n"), nil
}
