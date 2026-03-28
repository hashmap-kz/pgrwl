package storecrypt

import (
	"fmt"
	"os"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type SFTPConfig struct {
	// Required
	Host     string
	Port     string
	User     string
	PkeyPath string

	// Optional, it private key is created with a passphrase
	Passphrase string
}

type SFTPClient struct {
	sshClient  *ssh.Client
	sftpClient *sftp.Client

	config *SFTPConfig
}

// NewSFTPClient creates an SFTP client using passphrase-protected private key authentication
func NewSFTPClient(sftpConfig *SFTPConfig) (*SFTPClient, error) {
	var err error

	// Load the private key from file, or read from the property as a string
	key, err := os.ReadFile(sftpConfig.PkeyPath)
	if err != nil {
		return nil, fmt.Errorf("unable to read private key: %w", err)
	}

	// Parse the private key with passphrase
	var signer ssh.Signer
	if sftpConfig.Passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(sftpConfig.Passphrase))
		if err != nil {
			return nil, fmt.Errorf("unable to parse private key with passphrase: %w", err)
		}
	} else {
		signer, err = ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("unable to parse private key: %w", err)
		}
	}

	// Setup SSH configuration
	sshConfig := &ssh.ClientConfig{
		User: sftpConfig.User,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		//nolint:gosec
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	// Establish the SSH connection
	addr := fmt.Sprintf("%s:%s", sftpConfig.Host, sftpConfig.Port)
	conn, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to SFTP server: %w", err)
	}

	// Create an SFTP sftpClient over the SSH connection
	client, err := sftp.NewClient(conn)
	if err != nil {
		return nil, fmt.Errorf("unable to create SFTP sftpClient: %w", err)
	}

	return &SFTPClient{
		sshClient:  conn,
		sftpClient: client,
		config:     sftpConfig,
	}, nil
}

func (s *SFTPClient) SFTPClient() *sftp.Client {
	return s.sftpClient
}

func (s *SFTPClient) Close() error {
	var err error
	if s.sftpClient != nil {
		err = s.sftpClient.Close()
	}
	if s.sshClient != nil {
		err = s.sshClient.Close()
	}
	return err
}
