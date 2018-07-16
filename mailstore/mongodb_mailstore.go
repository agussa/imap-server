package mailstore

import (
	"errors"
	"fmt"
	"net/textproto"
	"time"
	"strings"
	_ "strconv"
	"github.com/jordwest/imap-server/types"
	"github.com/jordwest/imap-server/util"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	Data "github.com/gleez/smtpd/data"
)


// MongoDBMailstore is an in-memory mail storage for testing purposes and to
// provide an example implementation of a mailstore
type MongoDBMailstore struct {
	CurrentUser	*MongoDBUser
	Session  	*mgo.Session
	Users 		*mgo.Collection
	Messages 	*mgo.Collection
	Mailboxs 	*mgo.Collection
}

func newMongoDBMailbox(name string) *MongoDBMailbox {
	return &MongoDBMailbox{
		Tag:     name,
		messages: make([]Message, 0),
		NextID:  10,
	}
}

// NewMongoDBMailstore performs some initialisation and should always be
// used to create a new MongoDBMailstore
func NewMongoDBMailstore() *MongoDBMailstore {
	ms := &MongoDBMailstore{
		CurrentUser: &MongoDBUser{
			authenticated: false,
		},
		Session: nil,
		Users: nil,
	}
	var err error
	ms.Session, err = mgo.Dial("127.0.0.1:27017")
	if err != nil {
		fmt.Printf("Error connecting to MongoDB: %s", err)
	} else {
		ms.Users = ms.Session.DB("Smtpd").C("Users")		
		ms.Messages = ms.Session.DB("Smtpd").C("Messages")
		ms.Mailboxs = ms.Session.DB("Smtpd").C("Mailboxs")
	}
	return ms
}

// Authenticate implements the Authenticate method on the Mailstore interface
func (d *MongoDBMailstore) Authenticate(username string, password string) (User, error) {		
	ret:= d.Users.Find(bson.M{"username": username}).One(&d.CurrentUser)
	if ret != nil {
		return &MongoDBUser{}, errors.New("Invalid username")
	} else {
		if Data.Validate_Password(d.CurrentUser.Password, password) {
			d.CurrentUser.authenticated = true				

		} else {
			return &MongoDBUser{}, errors.New("Invalid username or password")
		}
	}	
	d.CurrentUser.mailstore = d
	
	return d.CurrentUser, nil
}

// MongoDBUser is an in-memory representation of a mailstore's user
type MongoDBUser struct {
	Data.User `bson:",inline"`
	authenticated bool
	mailstore     *MongoDBMailstore
}

// Mailboxes implements the Mailboxes method on the User interface
func (u *MongoDBUser) Mailboxes() []Mailbox {

	mbox  := []MongoDBMailbox {}

	u.mailstore.Mailboxs.Find(bson.M{"owner": u.Username}).Select(bson.M{"id":1, "owner":1, "name": 1, "nextID" : 1}).All(&mbox)
	if len(mbox) == 0 {
		u.mailstore.Mailboxs.Insert(&MongoDBMailbox{Id : 0, Tag : "INBOX", Owner : u.Username, NextID: 1})
		// u.mailstore.Mailboxs.Insert(&MongoDBMailbox{Id : 1, Tag : "Trash", Owner : u.Username, NextID: 9999})
		u.mailstore.Mailboxs.Find(bson.M{"owner": u.Username}).Select(bson.M{"id":1, "owner":1, "name": 1, "nextID" : 1}).All(&mbox)
	}
	mailboxes := make([]Mailbox, len(mbox))
	for i, element := range mbox {
		element.mailstore = u.mailstore
		element.messages = make([] Message, 0)
		mailboxes[i] = element
	}
	return mailboxes	
}

// MailboxByName returns a MongoDBMailbox object, given the mailbox's name
func (u *MongoDBUser) MailboxByName(name string) (Mailbox, error) {
	for _, mailbox := range u.Mailboxes() {
		if mailbox.Name() == name {
			return mailbox, nil
		}
	}
	return nil, errors.New("Invalid mailbox")
}

// MongoDBMailbox is an in-memory implementation of a Mailstore Mailbox

type MongoDBMailbox struct {
	Id        	uint32 `bson:"id"`
	Owner		string `bson:"owner"`
	Tag      	string `bson:"name"`
	NextID   	uint32 `bson:"nextID"`
	messages  	[]Message
	mailstore 	*MongoDBMailstore
}

// DebugPrintMailbox prints out all messages in the mailbox to the command line
// for debugging purposes
func (m MongoDBMailbox) DebugPrintMailbox() {
	// debugPrintMessagesMongo(m.messages)
}

// Name returns the Mailbox's name
func (m MongoDBMailbox) Name() string { return m.Tag }

// NextUID returns the UID that is likely to be assigned to the next
// new message in the Mailbox
func (m MongoDBMailbox) NextUID() uint32 { 
	mbox := MongoDBMailbox {}
	m.mailstore.Mailboxs.Find(bson.M{"owner": m.Owner, "id" : m.Id}).
		Select(bson.M{"id":1, "owner":1, "name": 1, "nextID" : 1}).
		One(&mbox)	
	return mbox.NextID
		// return 1
}
func (m MongoDBMailbox) IncrementID() uint32 { 
	mbox := MongoDBMailbox {}
	m.mailstore.Mailboxs.Find(bson.M{"owner": m.Owner, "id" : m.Id}).
		Select(bson.M{"id":1, "owner":1, "name": 1, "nextID" : 1}).
		One(&mbox)	
	m.mailstore.Mailboxs.Update(
		bson.M{"owner": m.Owner, "id" : m.Id}, 
		bson.M{"$set": bson.M{"nextID" : mbox.NextID +1}})

	return mbox.NextID
}

// LastUID returns the UID of the last message in the mailbox or if the
// mailbox is empty, the next expected UID
func (m MongoDBMailbox) LastUID() uint32 {	return 1 }

// Recent returns the number of messages in the mailbox which are currently
// marked with the 'Recent' flag
func (m MongoDBMailbox) Recent() uint32 {
	totalcurrent, _ := m.mailstore.Messages.Find(bson.M{"to.mailbox" : m.Owner, "mailboxid" : m.Id, "recent": true}).Count()
	return uint32(totalcurrent)
}

// Messages returns the total number of messages in the Mailbox
func (m MongoDBMailbox) Messages() uint32 { 	
	totalnew 		:= 0
	totalcurrent 	:=0
	totalcurrent, _ = m.mailstore.Messages.Find(bson.M{"to.mailbox" : m.Owner, "mailboxid" : m.Id}).Count()
	if m.Id==0 {
		totalnew, _ = m.mailstore.Messages.Find(bson.M{"to.mailbox" : m.Owner, "mailboxid" : nil}).Count()
		if totalnew > 0 {
			msgs := [] MyMessage {}
			m.mailstore.Messages.Find(bson.M{"to.mailbox" : m.Owner, "mailboxid" : nil, "nomor" : nil}).
			Select(bson.M{"id" : 1}).
			All(&msgs)
			for _, msg := range msgs {				
				m.mailstore.Messages.Update(
					bson.M{"id" : msg.Id}, 
					bson.M{"$set": bson.M{"nomor" : m.IncrementID(),
										  "mailboxid": m.Id}})
			}
		}
	}
	return uint32(totalnew+totalcurrent)
}

// Unseen returns the number of messages in the mailbox which are currently
// marked with the 'Unseen' flag
func (m MongoDBMailbox) Unseen() uint32 {
	totalnew := 0
	totalcurrent :=0
	if m.Id==0 {
		totalnew, _ = m.mailstore.Messages.Find(bson.M{"to.mailbox" : m.Owner, "unread": true, "mailboxid" : nil}).Count()
	}
	totalcurrent, _ = m.mailstore.Messages.Find(bson.M{"to.mailbox" : m.Owner, "unread": true, "mailboxid" : m.Id}).Count()
			
	return uint32(totalnew+totalcurrent)
}

// MessageBySequenceNumber returns a single message given the message's sequence number
func (m MongoDBMailbox) MessageBySequenceNumber(seqno uint32) Message {
	msg := MyMessage {}

	if err:=m.mailstore.Messages.Find(
		bson.M{"to.mailbox" : m.Owner, 
				"mailboxid" : m.Id, 
				"nomor" : seqno}).
		One(&msg); err==nil {
			msg.mailbox = &m
			return msg
		}
	return nil
}

// MessageByUID returns a single message given the message's sequence number
func (m MongoDBMailbox) MessageByUID(uidno uint32) Message {
	msg := MyMessage {}

	if err:=m.mailstore.Messages.Find(
		bson.M{"to.mailbox" : m.Owner, 
				"mailboxid" : m.Id, 
				"nomor" : uidno}).
		One(&msg); err==nil {
			msg.mailbox = &m
			return msg
		}
	return nil
}
func (m MongoDBMailbox) messageRangeBy(seqRange types.SequenceRange) []Message {
	msgs := []Message {} //make([]Message, 0)
	// If Min is "*", meaning the last UID in the mailbox, Max should
	// always be Nil
	if seqRange.Min.Last() {
		// Return the last message in the mailbox
		msg := MyMessage{}

		m.mailstore.Messages.Find(bson.M{"to.mailbox" : m.Owner, "mailboxid" : m.Id}).
			Sort("-nomor").
			Limit(1).
			One(&msg)
		msg.mailbox = &m
		msgs = append(msgs, msg)		
		return msgs
	}

	min, err := seqRange.Min.Value()
	if err != nil {
		fmt.Printf("Error: %s\n", err.Error())	
		return msgs
	}

	// If no Max is specified, the sequence number must be fixed
	if seqRange.Max.Nil() {
		var uid uint32
		// Fetch specific message by sequence number
		uid, err = seqRange.Min.Value()
		msg := m.MessageByUID(uid)
		if err != nil {
			fmt.Printf("Error: %s\n", err.Error())
			return msgs
		}
		if msg != nil {			
			msgs = append(msgs, msg)
		}
		return msgs
	}

	max, err := seqRange.Max.Value()
	if seqRange.Max.Last() {
		ms := [] MyMessage {}
		m.mailstore.Messages.Find(bson.M{"to.mailbox" : m.Owner, "mailboxid" : m.Id, "nomor" : 
				bson.M{"$gte": min}}).			
			Sort("-nomor").
			All(&ms)
		for _, msg := range ms {
			msg.mailbox = &m
			msgs = append(msgs, msg)	
		}
		
		return msgs
	} else {
		ms := [] MyMessage {}
		m.mailstore.Messages.Find(bson.M{"to.mailbox" : m.Owner, "mailboxid" : m.Id, "nomor" : 
			bson.M{"$gte" : min, "$lte" : max}}).			
			Sort("-nomor").
			All(&ms)

		for _, msg := range ms {
			msg.mailbox = &m
			msgs = append(msgs, msg)	
		}
	}
	return msgs
}


// MessageSetByUID returns a slice of messages given a set of UID ranges.
// eg 1,5,9,28:140,190:*
func (m MongoDBMailbox) MessageSetByUID(set types.SequenceSet) []Message {
	var msgs []Message

	// If the mailbox is empty, return empty array
	if m.Messages() == 0 {
		return msgs
	}

	for _, msgRange := range set {
		msgs = append(msgs, m.messageRangeBy(msgRange)...)
	}
	return msgs
}

// MessageSetBySequenceNumber returns a slice of messages given a set of
// sequence number ranges
func (m MongoDBMailbox) MessageSetBySequenceNumber(set types.SequenceSet) []Message {
	var msgs []Message
	if m.Messages() == 0 {
		return msgs
	}
	// For each sequence range in the sequence set
	for _, msgRange := range set {
		msgs = append(msgs, m.messageRangeBy(msgRange)...)
	}
	return msgs
}

func (m MongoDBMailbox) DeleteFlaggedMessages() ([]Message, error) {
	var delMsgs []Message

	ms := [] MyMessage {}
	m.mailstore.Messages.Find(bson.M{"to.mailbox" : m.Owner, "mailboxid" : m.Id, "deleted": true}).					
		All(&ms)
	for _, msg := range ms {
		delMsgs = append(delMsgs, msg)
		m.mailstore.Messages.Remove(msg);
	}
	
	return delMsgs, nil
}
// NewMessage creates a new message in the dummy mailbox.
func (m MongoDBMailbox) NewMessage() Message {
	return &MyMessage{				
		Created:		time.Now(),
		Starred: 		false,
		Unread: 		true,
		Recent: 		false,
		Deleted:        false,
		mailbox:		&m,
		Nomor: 			0,
		Mailboxid:		m.Id,
		body: 			"",
		hdrs:         make(textproto.MIMEHeader),
		// internalDate:   time.Now(),
		// flags:          types.Flags(0),
		// mailstore:      m.mailstore,
		// mailboxID:      m.ID,
		// body:           "",
	}
}


type MyMessage struct {
	//Data.Message	`bson:",inline"`
	Id 				string 	`bson:"id"`
	Nomor 			uint32 	`bson:"nomor"`
	Mailboxid 		uint32 	`bson:"mailboxid"`

	Created 		time.Time `bson:"created"`
	Starred 		bool 	`bson:"starred"`
	Unread 			bool 	`bson:"unread"`
	Deleted 		bool 	`bson:"deleted"`
	Recent 			bool 	`bson:"recent"`

	mailbox 		*MongoDBMailbox
	hdrs 			textproto.MIMEHeader
	body 			string 
}
func (m MyMessage) Header() (hdr textproto.MIMEHeader) {
	hdrs := make(textproto.MIMEHeader)	
	var msg *Data.Message = &Data.Message{}

	if err:=m.mailbox.mailstore.Messages.Find(bson.M{"to.mailbox":  m.mailbox.Owner, 
		"id" : m.Id}).One(msg); err==nil {

		// if value, isset := msg.Content.Headers["Received"]; isset==true {
		// 	hdrs.Set("Received", value[0])	
		// } 

		if value, isset := msg.Content.Headers["MIME-Version"]; isset==true {
			hdrs.Set("MIME-Version", value[0])	
		} 

		if value, isset := msg.Content.Headers["Return-Path"]; isset==true {
			hdrs.Set("Return-Path", value[0])	
		} 

		if value, isset := msg.Content.Headers["User-Agent"]; isset==true {
			hdrs.Set("User-Agent", value[0])	
		} 
		if value, isset := msg.Content.Headers["Subject"]; isset==true {
			hdrs.Set("Subject", value[0])	
		} else {
			hdrs.Set("Subject", msg.Subject)
		}
		if value, isset := msg.Content.Headers["Date"]; isset==true {
			hdrs.Set("Date", value[0])	
		} else {
			hdrs.Set("Date", msg.Created.Format(util.RFC822Date))
		}

		if value, isset := msg.Content.Headers["From"]; isset==true {
			hdrs.Set("From", value[0])	
		} else {
			hdrs.Set("From", msg.From.Mailbox + "@" + msg.From.Domain)			
		}
		if value, isset := msg.Content.Headers["To"]; isset==true {
			hdrs.Set("To", value[0])	
		} else {
			hdrs.Set("To", msg.To[0].Mailbox + "@" + msg.To[0].Domain)
		}

		if value, isset := msg.Content.Headers["Message-ID"]; isset==true {
			hdrs.Set("Message-ID", value[0])	
		}

		if value, isset := msg.Content.Headers["Content-Type"]; isset==true {
			hdrs.Set("Content-Type", value[0])	
		}
		if value, isset := msg.Content.Headers["Content-Language"]; isset==true {
			hdrs.Set("Content-Language", value[0])	
		}		
		if value, isset := msg.Content.Headers["Content-Transfer-Encoding"]; isset==true {
			hdrs.Set("Content-Transfer-Encoding", value[0])	
		}
	}
	return hdrs
}

// UID returns the message's unique identifier (UID).
func (m MyMessage) UID() uint32 { 
	// tm := m.Id[0:8]
	// if h, err := strconv.ParseUint(tm, 16, 32); err== nil {
		// return uint32(h)
	// }
	return m.Nomor
}

// SequenceNumber returns the message's sequence number.
func (m MyMessage) SequenceNumber() uint32 { return m.Nomor }

// Size returns the message's full RFC822 size, including full message header
// and body.
func (m MyMessage) Size() uint32 {
	hdrStr := fmt.Sprintf("%s\r\n", m.Header())
	return uint32(len(hdrStr)) + uint32(len(m.Body()))
}

// InternalDate returns the internally stored date of the message
func (m MyMessage) InternalDate() time.Time {
	// var msg *Data.Message = &Data.Message{}
	// if err:=m.mailbox.mailstore.Messages.Find(bson.M{"to.mailbox":  m.mailbox.Owner, 
	// 	"id" : m.Id}).One(msg); err==nil {
	// 	return msg.Created		
	// }
	// mailTime, _ := time.Parse("02-Jan-2006 15:04:05 -0700", "28-Oct-2014 00:09:00 +0700")
	// return mailTime
	return m.Created
}

// Body returns the full body of the message
func (m MyMessage) Body() string {
	msg := &Data.Message{}
	if err:=m.mailbox.mailstore.Messages.Find(bson.M{"to.mailbox":  m.mailbox.Owner, 
		"id" : m.Id}).One(msg); err==nil {		
		if strings.HasPrefix(m.Header().Get("Content-Type"), "text/plain") {
			return msg.Content.TextBody
		} else {
			return msg.Content.Body 
		}
		
	}
	return ""
}

// Keywords returns any keywords associated with the message
func (m MyMessage) Keywords() []string {
	var f []string
	//f[0] = "Test"
	return f
}

// Flags returns any flags on the message.
func (m MyMessage) Flags() types.Flags {
	var flag types.Flags	
	if !m.Unread {
		flag = flag.SetFlags(types.FlagSeen)
	} else {
		flag = flag.ResetFlags(types.FlagSeen)
	}
	if m.Starred {
		flag = flag.SetFlags(types.FlagFlagged)
	} else {
		flag = flag.ResetFlags(types.FlagFlagged)
	}
	if m.Deleted {
		flag = flag.SetFlags(types.FlagDeleted)
	} else {
		flag = flag.ResetFlags(types.FlagDeleted)	
	}
	if m.Recent {
		flag = flag.SetFlags(types.FlagRecent)
	} else {
		flag = flag.ResetFlags(types.FlagRecent)
	}
		
	return flag
}

// OverwriteFlags replaces any flags on the message with those specified.
func (m MyMessage) OverwriteFlags(newFlags types.Flags) Message {
	if (newFlags.HasFlags(types.FlagSeen)) {
		m.Unread = !newFlags.HasFlags(types.FlagSeen)
	}
	if (newFlags.HasFlags(types.FlagFlagged)) {
		m.Starred = newFlags.HasFlags(types.FlagFlagged)
	}
	if (newFlags.HasFlags(types.FlagRecent)) {
		m.Recent = newFlags.HasFlags(types.FlagRecent)
	}
	if (newFlags.HasFlags(types.FlagDeleted)) {
		m.Deleted = newFlags.HasFlags(types.FlagDeleted)
	}

	m.mailbox.mailstore.Messages.Update(bson.M{"id": m.Id}, bson.M{"$set": bson.M{
		"unread": m.Unread,
		"starred": m.Starred,
		"recent": m.Recent,
		"deleted": m.Deleted}})	
	return m
}

// AddFlags adds the given flag to the message.
func (m MyMessage) AddFlags(newFlags types.Flags) Message {
	if (newFlags.HasFlags(types.FlagSeen)) {
		m.Unread = false
	}
	if (newFlags.HasFlags(types.FlagFlagged)) {
		m.Starred = true
	}
	if (newFlags.HasFlags(types.FlagRecent)) {
		m.Recent = true
	}
	if (newFlags.HasFlags(types.FlagDeleted)) {
		m.Deleted = true
	}

	m.mailbox.mailstore.Messages.Update(bson.M{"id": m.Id}, bson.M{"$set": bson.M{
		"unread": m.Unread,
		"starred": m.Starred,
		"recent": m.Recent,
		"deleted": m.Deleted}})	

	return m
}

// RemoveFlags removes the given flag from the message.
func (m MyMessage) RemoveFlags(newFlags types.Flags) Message {
	
	if (newFlags.HasFlags(types.FlagSeen)) {
		m.Unread = true
	}
	if (newFlags.HasFlags(types.FlagFlagged)) {
		m.Starred = false
	}
	if (newFlags.HasFlags(types.FlagRecent)) {
		m.Recent = false
	}
	if (newFlags.HasFlags(types.FlagDeleted)) {
		m.Deleted = false
	}

	m.mailbox.mailstore.Messages.Update(bson.M{"id": m.Id}, bson.M{"$set": bson.M{
		"unread": m.Unread,
		"starred": m.Starred,
		"recent": m.Recent,
		"deleted": m.Deleted}})	
	return m
}

// SetHeaders sets the e-mail headers of the message.
func (m MyMessage) SetHeaders(newHeader textproto.MIMEHeader) Message {
	// m.hdrs = newHeader
	return m
}

// SetBody sets the body of the message.
func (m MyMessage) SetBody(newBody string) Message {
	// m.body = newBody
	return m
}

// Save saves the message to the mailbox it belongs to.
func (m MyMessage) Save() (Message, error) {
	fmt.Printf("MESGE SAVE: %+v\n", m)
	// mailbox := m.mailstore.User.mailboxes[m.mailboxID]
	// if m.sequenceNumber == 0 {
	// 	// Message is new
	// 	m.uid = mailbox.NextUID
	// 	mailbox.NextUID++
	// 	m.sequenceNumber = uint32(len(mailbox.messages))
	// 	mailbox.messages = append(mailbox.messages, m)
	// } else {
	// 	// Message exists
	// 	mailbox.messages[m.sequenceNumber-1] = m
	// }
	return m, nil
}



func debugPrintMessagesMongo(messages []Message) {
	fmt.Printf("SeqNo  |UID    |From      |To        |Subject\n")
	fmt.Printf("-------+-------+----------+----------+-------\n")
	for _, msg := range messages {
		from := msg.Header().Get("from")
		to := msg.Header().Get("to")
		subject := msg.Header().Get("subject")
		fmt.Printf("%-7d|%-7d|%-10.10s|%-10.10s|%s\n", msg.SequenceNumber(), msg.UID(), from, to, subject)
	}
}
// DeleteFlaggedMessages deletes messages marked with the Delete flag and
// returns them.
