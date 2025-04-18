package main

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
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

	//copy and save the file to temporary file path
	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error creating temp file", err)
		return
	}

	//defer removing temp dir
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	//copy contents from the wire to the temp file
	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error copying video to server", err)
		return
	}
	//reset the tempfiles file pointer to beginning
	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error resetting pointer to beginning", err)
		return
	}
	//check aspect ratio
	aspectRatio, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error retrieving Aspect Ratio", err)
		return
	}
	videoOrientation := ""
	if aspectRatio == "16:9" {
		videoOrientation = "landscape"
	} else if aspectRatio == "9:16" {
		videoOrientation = "portrait"
	} else {
		videoOrientation = "other"
	}

	//copy to new path, move moov atom to front
	fastStartPath, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to process video for fast start", err)
		return
	}

	fastStartFile, err := os.Open(fastStartPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to open processed video", err)
		return
	}
	defer fastStartFile.Close()
	defer os.Remove(fastStartPath)

	//convert file name to unique key, add dyanmic prefix based on orientation
	key := getAssetPath(fileHeader)
	key = filepath.Join(videoOrientation, key)
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(key),
		Body:        fastStartFile,
		ContentType: aws.String(fileHeader),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't forward to server", err)
		return
	}

	//videoURL := cfg.getObjectURL(key)
	//
	//pointer to  video string in video db.url
	videoURL := fmt.Sprintf("%s,%s", cfg.s3Bucket, key)
	video.VideoURL = &videoURL

	//update db with video url string
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to update video", err)
		return
	}

	video, err = cfg.dbVideoToSignedVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to update URL", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	newClient := s3.NewPresignClient(s3Client)
	httpRequest, err := newClient.PresignGetObject(context.Background(), &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}, s3.WithPresignExpires(expireTime))

	if err != nil {
		return "", fmt.Errorf("error presigning object: %w", err)
	}

	return httpRequest.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil
	}
	//why return nils here?
	urlSplit := strings.Split(*video.VideoURL, ",")
	if len(urlSplit) != 2 {
		return video, nil
	}

	bucket := urlSplit[0]
	key := urlSplit[1]

	presignedURL, err := generatePresignedURL(cfg.s3Client, bucket, key, time.Hour)
	if err != nil {
		return video, err
	}
	video.VideoURL = &presignedURL
	return video, nil
}
