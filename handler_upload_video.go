package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	//set upload limit of 1gb
	const maxMemory = 10 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxMemory)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}
	//get video metadata
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to retrieve video data", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "User is not owner of Video", err)
		return
	}
	//parse the video file from form data
	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()
	//validate video type
	fileHeader, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to parse media type", err)
		return
	}
	if fileHeader != "video/mp4" {
		respondWithError(w, http.StatusUnsupportedMediaType, "Video must be mp4", nil)
		return
	}
	fileType := stripContentTypeVideo(fileHeader)
	//validate video further by verifying the video bytes for mp4- ftyp
	buf := make([]byte, 12) // or larger, depending on what you want to inspect
	_, err = file.Read(buf)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error reading file header", err)
		return
	}

	// Check if the file contains "ftyp" in the expected location
	if !bytes.Contains(buf, []byte("ftyp")) {
		respondWithError(w, http.StatusUnsupportedMediaType, "File is not a valid MP4", nil)
		return
	}

	//use system default for file path directory, for temporary storage
	filePath := filepath.Join("", "tubely-upload.mp4")
	//copy and save the file to temporary file path
	newFile, err := os.Create(filePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error creating temp file", err)
		return
	}
	//defer removing temp dir
	defer os.Remove(filePath)
	//defer closing temp video
	defer newFile.Close()
	//copy contents from the wire to the temp file
	_, err = io.Copy(newFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error copying video to server", err)
		return
	}
	//reset the tempfiles file pointer to beginning
	_, err = newFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error resetting pointer to beginning", err)
		return
	}
	//put object in s3
	bucketName := "tubely-73439"
	//random name for key + .filetype
	videoNameByte := make([]byte, 32)
	rand.Read(videoNameByte)
	encodeRawVideoName := base64.RawURLEncoding.EncodeToString(videoNameByte)
	videoFileName := encodeRawVideoName + "." + fileType
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &bucketName,
		Key:         &videoFileName,
		Body:        newFile,
		ContentType: &fileHeader,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't forward to server", err)
		return
	}

	videoURL := "https://" + bucketName + ".s3." + cfg.s3Region + ".amazonaws.com/" + videoFileName
	//pointer to  video string in video db.url
	video.VideoURL = &videoURL
	//update db with video url string
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
