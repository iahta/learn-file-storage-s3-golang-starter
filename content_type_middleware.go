package main

import (
	"strings"
)

func stripContentTypeImage(content string) string {
	contentSplit := strings.Split(content, "/")
	return contentSplit[1]
}

func stripContentTypeVideo(content string) string {
	contentSplit := strings.Split(content, "/")
	return contentSplit[1]
}
