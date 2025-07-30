package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

type ffprobeOutput struct {
	Streams []struct {
		Index       int    `json:"index"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		AspectRatio string `json:"display_aspect_ratio"`
	} `json:"streams"`
}

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
	fmt.Println("filename: ", newFile.Name())
	aspectRatio, err := getVideoAspectRatio(newFile.Name())
	// aspectRatio := "16:9" // hardcoded for windows
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to get the aspect ratio of video", err)
		return
	}

	newVidPath, err := processVideoFastStart(newFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to have preprocess ", err)
		return
	}
	processedVidFile, err := os.Open(newVidPath)
	defer processedVidFile.Close()
	defer os.Remove(newVidPath)

	fileS3Dir := ""
	switch aspectRatio {
	case "16:9":
		fileS3Dir = "landscape"
	case "9:16":
		fileS3Dir = "portrait"
	default:
		fileS3Dir = "other"
	}

	randomBytes := make([]byte, 32)
	rand.Read(randomBytes)
	videoKey := fmt.Sprintf("%v/%v.%v", fileS3Dir, base64.RawURLEncoding.EncodeToString(randomBytes), fileExt)
	fmt.Println("video key: ", videoKey)
	_, err = cfg.s3Client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &videoKey,
		Body:        processedVidFile,
		ContentType: &videoType,
	})

	if err != nil {
		log.Printf("%v", err)
		respondWithError(w, http.StatusInternalServerError, "unable to send the file to S3", err)
		return
	}
	fmt.Println(newVidPath)

	newVideoURL := fmt.Sprintf("https://%v.s3.%v.amazonaws.com/%v", cfg.s3Bucket, cfg.s3Region, videoKey)
	curVideo.VideoURL = &newVideoURL
	cfg.db.UpdateVideo(curVideo)
	w.WriteHeader(204)
}

func getVideoAspectRatio(filePath string) (string, error) {
	// workingDir, err:=os.Getwd()
	// if err!=nil {
	// 	return "",err
	// }
	ffprobeCmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	output := bytes.Buffer{}
	ffprobeCmd.Stdout = &output
	err := ffprobeCmd.Run()
	if err != nil {
		return "", err
	}

	jsonOutput := ffprobeOutput{}
	err = json.Unmarshal(output.Bytes(), &jsonOutput)
	if err != nil {
		return "", err
	}

	potentialResult := jsonOutput.Streams[0].AspectRatio
	if potentialResult != "9:16" && potentialResult != "16:9" {
		return "other", nil
	}
	return potentialResult, nil
}

func processVideoFastStart(filePath string) (string, error) {
	inputFile := strings.Split(filePath, ".")
	outputFilePath := fmt.Sprintf("%v.processed.%v", inputFile[0], inputFile[1])
	ffmpegCmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilePath)
	err := ffmpegCmd.Run()
	if err != nil {
		return "", err
	}

	return outputFilePath, nil
}
