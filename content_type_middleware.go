package main

import "strings"

func stripContentType(content string) string {
	contentSplit := strings.Split(content, "/")
	return contentSplit[1]
}
