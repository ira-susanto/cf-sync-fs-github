# cf-sync-fs-github
Cloud function to sync firestore entry to github upon create, update, or delete

## Deployment
1. Temporarily comment out `vendor` from .gitignore
2. Run `go mod vendor`
3. Deploy to cloud function
     ```
     gcloud functions deploy SyncFirestoreToGithub \
        --project <<GOOGLE_PROJECT_ID>> \
        --region us-central1 \
        --runtime go121 \
        --trigger-event "providers/cloud.firestore/eventTypes/document.write" \
        --trigger-resource <<FIRESTORE_DOCUMENT_PATH>> \
        --env-vars-file=.env.yaml \
        --docker-registry artifact-registry
     ```