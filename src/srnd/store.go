//
// store.go
//

package srnd

import (
	"bufio"
	"bytes"
	"crypto/sha512"
	"errors"
	"github.com/majestrate/nacl"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ArticleStore interface {
	MessageReader
	MessageWriter

	// get the filepath for an attachment
	AttachmentFilepath(fname string) string
	// get the filepath for an attachment's thumbnail
	ThumbnailFilepath(fname string) string
	// do we have this article?
	HasArticle(msgid string) bool
	// create a file for a message
	CreateFile(msgid string) io.WriteCloser
	// create a file for a temp message, returns nil if it's already open
	CreateTempFile(msgid string) io.WriteCloser
	// get the filename of a message
	GetFilename(msgid string) string
	// get the filename of a temp message
	GetTempFilename(msgid string) string
	// Get a message given its messageid
	GetMessage(msgid string) NNTPMessage
	// get a temp message given its messageid
	// temp message is deleted once read
	ReadTempMessage(msgid string) NNTPMessage
	// store a post
	StorePost(nntp NNTPMessage) error
	// get article headers only
	GetHeaders(msgid string) ArticleHeaders
	// get our temp directory for articles
	TempDir() string
	// get a list of all the attachments we have
	GetAllAttachments() ([]string, error)
	// generate a thumbnail
	GenerateThumbnail(fname string) error
}
type articleStore struct {
	directory    string
	temp         string
	attachments  string
	thumbs       string
	database     Database
	convert_path string
	ffmpeg_path  string
	sox_path     string
}

func createArticleStore(config map[string]string, database Database) ArticleStore {
	store := &articleStore{
		directory:    config["store_dir"],
		temp:         config["incoming_dir"],
		attachments:  config["attachments_dir"],
		thumbs:       config["thumbs_dir"],
		convert_path: config["convert_bin"],
		ffmpeg_path:  config["ffmpegthumbnailer_bin"],
		sox_path:     config["sox_bin"],
		database:     database,
	}
	store.Init()
	return store
}

func (self *articleStore) TempDir() string {
	return self.temp
}

// initialize article store
func (self *articleStore) Init() {
	EnsureDir(self.directory)
	EnsureDir(self.temp)
	EnsureDir(self.attachments)
	EnsureDir(self.thumbs)
	if !CheckFile(self.convert_path) {
		log.Fatal("cannot find executable for convert: ", self.convert_path, " not found")
	}
	if !CheckFile(self.ffmpeg_path) {
		log.Fatal("connt find executable for ffmpegthumbnailer: ", self.ffmpeg_path, " not found")
	}
	if !CheckFile(self.sox_path) {
		log.Fatal("connt find executable for sox: ", self.sox_path, " not found")
	}
}

func (self *articleStore) isAudio(fname string) bool {
	for _, ext := range []string{".mp3", ".ogg", ".oga", ".opus", ".flac", ".m4a"} {
		if strings.HasSuffix(strings.ToLower(fname), ext) {
			return true
		}
	}
	return false
}

// is this an image format we need convert for?
func (self *articleStore) isImage(fname string) bool {
	for _, ext := range []string{".gif", ".ico", ".png", ".jpeg", ".jpg", ".png", ".webp"} {
		if strings.HasSuffix(strings.ToLower(fname), ext) {
			return true
		}
	}
	return false

}

func (self *articleStore) GenerateThumbnail(fname string) error {
	outfname := self.ThumbnailFilepath(fname)
	infname := self.AttachmentFilepath(fname)
	var cmd *exec.Cmd
	if self.isImage(fname) {
		cmd = exec.Command(self.convert_path, "-thumbnail", "200", infname, outfname)
	} else if self.isAudio(fname) {
		tmpfname := infname + ".wav"
		cmd = exec.Command(self.ffmpeg_path, "-i", infname, tmpfname)
		exec_out, err := cmd.CombinedOutput()
		defer DelFile(tmpfname)
		if err == nil {
			cmd = exec.Command(self.sox_path, tmpfname, "-n", "spectrogram", "-a", "-d", "0:30", "-r", "-p", "6", "-x", "200", "-y", "150", "-o", outfname)
			exec_out, err = cmd.CombinedOutput()
		}
		if err != nil {
			log.Println("error generating audio thumbnail", err, string(exec_out))
		}
		return err
	} else {
		cmd = exec.Command(self.ffmpeg_path, "-i", infname, "-vf", "scale=300:200", "-vframes", "1", outfname)
	}
	exec_out, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("error generating thumbnail", string(exec_out))
	}
	return err
}

func (self *articleStore) GetAllAttachments() (names []string, err error) {
	var f *os.File
	f, err = os.Open(self.attachments)
	if err == nil {
		names, err = f.Readdirnames(0)
	}
	return
}

func (self *articleStore) ReadMessage(r io.Reader) (NNTPMessage, error) {
	return read_message(r)
}

func (self *articleStore) StorePost(nntp NNTPMessage) (err error) {

	f := self.CreateFile(nntp.MessageID())
	if f != nil {
		err = self.WriteMessage(nntp, f)
		f.Close()
	}

	nntp_inner := nntp.Signed()
	if nntp_inner == nil {
		// no inner article
		// store the data in the article
		self.database.RegisterArticle(nntp)
		for _, att := range nntp.Attachments() {
			// save attachments
			go self.saveAttachment(att)
		}
	} else {
		// we have inner data
		// store the signed data
		self.database.RegisterArticle(nntp_inner)
		// record a tripcode
		self.database.RegisterSigned(nntp.MessageID(), nntp.Pubkey())
		for _, att := range nntp_inner.Attachments() {
			go self.saveAttachment(att)
		}
	}
	return
}

// save an attachment
func (self *articleStore) saveAttachment(att NNTPAttachment) {
	var err error
	var f io.WriteCloser
	fpath := att.Filepath()
	upload := self.AttachmentFilepath(fpath)
	thumb := self.ThumbnailFilepath(fpath)
	if CheckFile(upload) {
		log.Println("already have file", fpath)
		if !CheckFile(thumb) && att.NeedsThumbnail() {
			log.Println("create thumbnail for", fpath)
			err = self.GenerateThumbnail(fpath)
			if err != nil {
				log.Println("failed to generate thumbnail", err)
			}
		}
		return
	}
	// save attachment
	log.Println("save attachment", att.Filename(), "to", upload)
	f, err = os.Create(upload)
	if err == nil {
		_, err = io.Copy(f, att)
		f.Close()
	}
	if err != nil {
		log.Println("did not save attachment", err)
		return
	}

	// generate thumbanils
	if att.NeedsThumbnail() {
		log.Println("create thumbnail for", fpath)
		err = self.GenerateThumbnail(fpath)
		if err != nil {
			log.Println("failed to generate thumbnail", err)
		}
	}
}

// eh this isn't really needed is it?
func (self *articleStore) WriteMessage(nntp NNTPMessage, wr io.Writer) (err error) {
	return nntp.WriteTo(wr, "\n")
}

// get the filepath for an attachment
func (self *articleStore) AttachmentFilepath(fname string) string {
	return filepath.Join(self.attachments, fname)
}

// get the filepath for a thumbanil
func (self *articleStore) ThumbnailFilepath(fname string) string {
	// all thumbnails are jpegs now
	if strings.HasSuffix(fname, ".gif") {
		return filepath.Join(self.thumbs, fname)
	}
	return filepath.Join(self.thumbs, fname+".jpg")
}

// create a file for this article
func (self *articleStore) CreateFile(messageID string) io.WriteCloser {
	fname := self.GetFilename(messageID)
	file, err := os.Create(fname)
	if err != nil {
		log.Println("cannot open file", fname)
		return nil
	}
	return file
}

// create a temp file for inboud articles
func (self *articleStore) CreateTempFile(messageID string) io.WriteCloser {
	fname := self.GetTempFilename(messageID)
	if CheckFile(fname) {
		log.Println(fname, "already open")
		return nil
	}
	file, err := os.Create(fname)
	if err != nil {
		log.Println("cannot open file", fname)
		return nil
	}
	return file
}

// return true if we have an article
func (self *articleStore) HasArticle(messageID string) bool {
	return CheckFile(self.GetFilename(messageID))
}

// get the filename for this article
func (self *articleStore) GetFilename(messageID string) string {
	if !ValidMessageID(messageID) {
		log.Println("!!! bug: tried to open invalid message", messageID, "!!!")
		return ""
	}
	return filepath.Join(self.directory, messageID)
}

// get the filename for this article
func (self *articleStore) GetTempFilename(messageID string) string {
	if !ValidMessageID(messageID) {
		log.Println("!!! bug: tried to open invalid temp message", messageID, "!!!")
		return ""
	}
	return filepath.Join(self.temp, messageID)
}

// loads temp message and deletes old article
func (self *articleStore) ReadTempMessage(messageID string) NNTPMessage {
	fname := self.GetTempFilename(messageID)
	nntp := self.readfile(fname)
	DelFile(fname)
	return nntp
}

// read a file give filepath
func (self *articleStore) readfile(fname string) NNTPMessage {

	file, err := os.Open(fname)
	if err != nil {
		log.Println("store cannot open file", fname)
		return nil
	}
	message, err := self.ReadMessage(file)
	file.Close()
	if err == nil {
		return message
	}

	log.Println("failed to load file", fname)
	return nil
}

// get the replies for a thread
func (self *articleStore) GetThreadReplies(messageID string, last int) []NNTPMessage {
	var repls []NNTPMessage
	if self.database.ThreadHasReplies(messageID) {
		rpls := self.database.GetThreadReplies(messageID, 0, last)
		if rpls == nil {
			return repls
		}
		for _, rpl := range rpls {
			msg := self.GetMessage(rpl)
			if msg == nil {
				log.Println("cannot get message", rpl)
			} else {
				repls = append(repls, msg)
			}
		}
	}
	return repls
}

// load an article
// return nil on failure
func (self *articleStore) GetMessage(messageID string) NNTPMessage {
	return self.readfile(self.GetFilename(messageID))
}

// get article with headers only
func (self *articleStore) GetHeaders(messageID string) ArticleHeaders {
	// TODO: don't load the entire body
	nntp := self.readfile(self.GetFilename(messageID))
	if nntp == nil {
		return nil
	}
	return nntp.Headers()
}

func read_message(r io.Reader) (NNTPMessage, error) {

	msg, err := mail.ReadMessage(r)
	nntp := new(nntpArticle)

	if err == nil {
		nntp.headers = ArticleHeaders(msg.Header)
		content_type := nntp.ContentType()
		media_type, params, err := mime.ParseMediaType(content_type)
		if err != nil {
			log.Println("failed to parse media type", err, "for mime", content_type)
			return nil, err
		}
		boundary, ok := params["boundary"]
		if ok {
			partReader := multipart.NewReader(msg.Body, boundary)
			for {
				part, err := partReader.NextPart()
				if err == io.EOF {
					return nntp, nil
				} else if err == nil {
					hdr := part.Header
					// get content type of part
					part_type := hdr.Get("Content-Type")
					// parse content type
					media_type, _, err = mime.ParseMediaType(part_type)
					if err == nil {
						if media_type == "text/plain" {
							att := readAttachmentFromMimePart(part)
							if att != nil {
								nntp.message = att
							}
						} else {
							// non plaintext gets added to attachments
							att := readAttachmentFromMimePart(part)
							if att != nil {
								nntp.Attach(att)
							}
						}
					} else {
						log.Println("part has no content type", err)
					}
					part.Close()
				} else {
					log.Println("failed to load part! ", err)
					return nil, err
				}
			}
		} else if media_type == "message/rfc822" {
			// tripcoded message
			sig := nntp.headers.Get("X-Signature-Ed25519-Sha512", "")
			pk := nntp.Pubkey()
			if pk == "" || sig == "" {
				log.Println("invalid sig or pubkey", sig, pk)
				return nil, errors.New("invalid headers")
			}
			log.Printf("got signed message from %s", pk)
			pk_bytes := unhex(pk)
			sig_bytes := unhex(sig)
			signed_body := new(bytes.Buffer)
			nntp.signedPart = &nntpAttachment{
				body: signed_body,
			}
			h := sha512.New()
			var buff bytes.Buffer
			mw := io.MultiWriter(signed_body, &buff)
			_, err := io.Copy(mw, msg.Body)
			if err != nil {
				log.Println("error reading signed body", err)
				return nil, err
			}
			r := bufio.NewReader(&buff)
			crlf := []byte{13, 10}
			line, err := r.ReadBytes('\n')
			if err != nil {
				return nil, err
			}
			h.Write(line[:len(line)-1])
			for {
				line, err := r.ReadBytes('\n')
				if err == io.EOF {
					break
				}
				h.Write(crlf)
				h.Write(line[:len(line)-1])
			}
			buff.Reset()
			hash := h.Sum(nil)
			log.Printf("hash=%s", hexify(hash))
			log.Printf("sig=%s", hexify(sig_bytes))
			if nacl.CryptoVerifyFucky(hash, sig_bytes, pk_bytes) {
				log.Println("signature is valid :^)")
				return nntp, nil
			} else {
				log.Println("!!!signature is invalid!!!")
			}
		} else {
			// plaintext attachment
			var buff bytes.Buffer
			_, err = io.Copy(&buff, msg.Body)
			nntp.message = createPlaintextAttachment(buff.String())
			buff.Reset()
			return nntp, err
		}
	} else {
		log.Println("failed to read message", err)
		return nil, err
	}
	return nntp, err
}
