package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

const maxMemory = 10 << 20

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// TODO: implement the upload here

	r.ParseMultipartForm(int64(maxMemory))                // parsing the data
	imageData, fileHeader, err := r.FormFile("thumbnail") // taking the data we receive from the form
	if err != nil {
		respondWithError(w, 500, "couldn't get the data from form", err)
		return
	}
	defer imageData.Close()
	mediaType := fileHeader.Header.Get("Content-Type") // things are handled differently than the simple JSON requests
	fileType, _, err := mime.ParseMediaType(mediaType)
	fmt.Println("fileTypes: ", fileType, mediaType)
	if fileType != "image/jpeg" && fileType != "image/png" {
		respondWithError(w, 400, "the provided file doesn't have the right format", err)
		return
	}
	fileExt := strings.Split(fileType, "/")[1]
	newfilename := make([]byte, 32)
	rand.Read(newfilename) // populates the all slice with random bytes
	b64EncodedFilename := base64.RawURLEncoding.EncodeToString(newfilename)

	videoMetadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, 500, "couldn't get the data from DB", err)
		return
	}
	if videoMetadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "user not authorized to access the resource", err)
		return
	}

	newThumbnailURL := fmt.Sprintf("%v/%v.%v", cfg.assetsRoot, b64EncodedFilename, fileExt)
	createdFile, err := os.Create(newThumbnailURL) // creating the new file in the FS
	if err != nil {
		fmt.Printf("%v\n", err)
		respondWithError(w, 500, "unable to create file", err)
		return
	}
	io.Copy(createdFile, imageData) // populating the new file content
	thumbnailMetadataURL := fmt.Sprintf("http://localhost:%v/assets/%v.%v", cfg.port, b64EncodedFilename, fileExt)
	videoMetadata.ThumbnailURL = &thumbnailMetadataURL
	cfg.db.UpdateVideo(videoMetadata)

	respondWithJSON(w, http.StatusOK, videoMetadata)
}
