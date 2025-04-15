package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
)

func getVideoAspectRatio(filePath string) (string, error) {
	dir, _ := os.Getwd()
	log.Printf("Current working directory: %s", dir)
	log.Printf("File Path: %s", filePath)
	log.Printf("\n")
	type videoDimension struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}
	var b bytes.Buffer
	dimension := videoDimension{}
	command := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var stderr bytes.Buffer
	command.Stdout = &b
	command.Stderr = &stderr
	err := command.Run()
	if err != nil {
		log.Printf("ffprobe error: %s", stderr.String())
		return "", fmt.Errorf("error running command: %v", err)
	}
	err = json.Unmarshal(b.Bytes(), &dimension)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling video dimensions: %w", err)
	}
	if len(dimension.Streams) == 0 {
		return "", fmt.Errorf("no streams found in video file")
	}
	width := float64(dimension.Streams[0].Width)
	height := float64(dimension.Streams[0].Height)
	ratio := width / height
	const tolerance = 0.01
	landscapeRatio := 16.0 / 9.0
	portraitRatio := 9.0 / 16.0

	if math.Abs(ratio-landscapeRatio) < tolerance {
		return "16:9", nil
	} else if math.Abs(ratio-portraitRatio) < tolerance {
		return "9:16", nil
	} else {
		return "other", nil
	}

}
