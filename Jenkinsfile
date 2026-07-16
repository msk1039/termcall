pipeline {
    agent any

    stages {
        stage('Checkout') {
            steps {
                checkout scm
            }
        }
        
        stage('Deploy termcall-server') {
            steps {
                script {
                    echo 'Deploying termcall-server via docker compose...'
                    sh 'cp /tmp/.env .env || echo "Warning: No .env file found mounted from host"'
                    sh 'docker compose up -d --build termcall-server'
                }
            }
        }
    }
    
    post {
        success {
            echo 'Deployment finished successfully!'
        }
        failure {
            echo 'Deployment failed!'
        }
    }
}
