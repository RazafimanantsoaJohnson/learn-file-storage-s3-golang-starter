package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"math/rand"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	maxVideoSize := 1 << 30
	http.MaxBytesReader(w, r.Body, int64(maxVideoSize)) // will send an error if > to defined size
	videoIdParam := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIdParam)
	if err != nil {
		log.Printf("%v\n", err)
		w.WriteHeader(500)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, 401, "User not authorized", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "User not authorized", err)
		return
	}

	curVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get the video from Database", err)
		return
	}
	if curVideo.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "authentified user not authorized to access this resource", err)
		return
	}

	videoData, videoHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to get the file data", err)
		return
	}
	defer videoData.Close()

	fileType := videoHeader.Header.Get("Content-Type")
	videoType, _, err := mime.ParseMediaType(fileType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "unable to parse the type of the provided file", err)
		return
	}
	if videoType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "invalid video type", err)
		return
	}
	fileExt := strings.Split(videoType, "/")[1]

	newFile, err := os.CreateTemp("", "tubely-upload."+fileExt)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to handle the file", err)
		return
	}
	defer os.Remove("tubely-upload." + fileExt)

	_, err = io.Copy(newFile, videoData)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to handle the file", err)
		return
	}

	newFile.Seek(0, io.SeekStart) //move the pointer of the file back to beginning
	randomBytes := make([]byte, 32)
	rand.Read(randomBytes)
	videoKey := fmt.Sprintf("%v.%v", base64.RawURLEncoding.EncodeToString(randomBytes), fileExt)
	cfg.s3Client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &videoKey,
		Body:        newFile,
		ContentType: &videoType,
	})

	newVideoURL := fmt.Sprintf("https://%v.s3.%v.amazonaws.com/%v", cfg.s3Bucket, cfg.s3Region, videoKey)
	curVideo.VideoURL = &newVideoURL
	cfg.db.UpdateVideo(curVideo)
	w.WriteHeader(204)
}
