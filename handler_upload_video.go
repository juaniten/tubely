package main

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	maxBytes := 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))

	videoIdParameter := r.PathValue("videoID")
	if videoIdParameter == "" {
		respondWithError(w, http.StatusBadRequest, "videoID required", nil)
		return
	}
	videoUUID, err := uuid.Parse(videoIdParameter)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to parse video ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid bearer token", err)
		return
	}
	userId, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Bearer token", err)
		return
	}

	video, err := cfg.db.GetVideo(videoUUID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to retrieve video", err)
		return
	}
	if video.UserID != userId {
		respondWithError(w, http.StatusUnauthorized, "User is not video owner", err)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "File sent is not a video", err)
		return
	}
	defer file.Close()

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type, only MP4 is allowed", nil)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload-*.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error creating temporary file", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error copying temporary file", err)
		return
	}

	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error resetting temp file pointer", err)
		return
	}

	processedVideoFilepath, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error processing video", err)
		return
	}
	processedVideo, err := os.Open(processedVideoFilepath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error opening processed video", err)
	}
	defer processedVideo.Close()

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error opening processed video", err)
		return
	}

	key, err := cfg.getS3KeyFromFile(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error generating key for the video", err)
		return
	}
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &key,
		Body:        processedVideo,
		ContentType: aws.String(mediaType),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error uploading to S3", err)
		return
	}

	cloudfrontUrl := fmt.Sprintf("%s/%s", cfg.s3CfDistribution, key)
	video.UpdatedAt = time.Now()
	video.VideoURL = &cloudfrontUrl
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error updating video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}

func processVideoForFastStart(filePath string) (string, error) {
	outputFilePath := filePath + ".processing"
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilePath)
	err := cmd.Run()
	if err != nil {
		return "", errors.New("error processing video for fast start")
	}
	return outputFilePath, nil
}
