# Running the end-to-end tests for Cloud Logging

The cloud_logger_opts_test.go is an end-to-end test that uses the guest-logging-go logger to write logs to Cloud Logging and read them back to verify the correct behavior of the library. To run it, you need access to a Google Cloud project where you can read and write logs. The test requires the PROJECT_NAME environment variable to be set, and it uses Google Application Default Credentials to access Cloud Logging. If you want to run the test with a Google service account, you try the following setup:

Set these variables:
```bash
PROJECT_NAME=
```

Perform one-time setup for the service account:
```bash
gcloud iam service-accounts create test-guest-logging-go \
	--project ${PROJECT_NAME:?}
mkdir -p ~/keys
# It may be necessary to download a new service account key when it expires.
gcloud iam service-accounts keys create ~/keys/test-guest-logging-go.json \
	--iam-account=test-guest-logging-go@${PROJECT_NAME:?}.gserviceaccount.com \
	--project ${PROJECT_NAME}
gcloud projects add-iam-policy-binding ${PROJECT_NAME:?} \
	--member=serviceAccount:test-guest-logging-go@${PROJECT_NAME:?}.iam.gserviceaccount.com \
	--role=roles/logging.viewer
gcloud projects add-iam-policy-binding ${PROJECT_NAME:?} \
	--member=serviceAccount:test-guest-logging-go@${PROJECT_NAME:?}.iam.gserviceaccount.com \
	--role=roles/logging.logWriter
```

Run the tests:
```bash
GOOGLE_APPLICATION_CREDENTIALS=~/keys/test-guest-logging-go.json \
PROJECT_NAME=${PROJECT_NAME:?} \
	go test -v ./...
```