package main

import (
	"strings"
)

func stripContentTypeImage(content string) string {
	contentSplit := strings.Split(content, "/")
	return contentSplit[1]
}
