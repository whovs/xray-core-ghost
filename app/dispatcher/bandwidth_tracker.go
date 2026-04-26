package dispatcher

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/errors"
	corenet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/transport"
)

// BandwidthRecord holds the tracked bandwidth data for a connection
type BandwidthRecord struct {
	User        string    `json:"user"`
	Domain      string    `json:"domain"`
	InboundTag  string    `json:"inboundTag"`
	OutboundTag string    `json:"outboundTag,omitempty"`
	SourceIP    string    `json:"sourceIP,omitempty"`
	SourcePort  int       `json:"sourcePort,omitempty"`
	DestPort    int       `json:"destPort,omitempty"`
	Network     string    `json:"network,omitempty"`
	Protocol    string    `json:"protocol,omitempty"`
	UpBytes     int64     `json:"upBytes"`
	DownBytes   int64     `json:"downBytes"`
	Duration    int64     `json:"duration"`
	Timestamp   time.Time `json:"timestamp"`
}

// CountingReader wraps a buf.Reader and counts bytes read
type CountingReader struct {
	reader buf.TimeoutReader
	count  int64
	startTime time.Time
	closed    int32
	mu        sync.Mutex
}

// NewCountingReader creates a new CountingReader wrapping the given reader
func NewCountingReader(reader buf.TimeoutReader) *CountingReader {
	return &CountingReader{
		reader:    reader,
		startTime: time.Now(),
	}
}

// ReadMultiBuffer implements buf.Reader
func (r *CountingReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb, err := r.reader.ReadMultiBuffer()
	if !mb.IsEmpty() {
		atomic.AddInt64(&r.count, int64(mb.Len()))
	}
	return mb, err
}

// ReadMultiBufferTimeout implements buf.TimeoutReader
func (r *CountingReader) ReadMultiBufferTimeout(timeout time.Duration) (buf.MultiBuffer, error) {
	mb, err := r.reader.ReadMultiBufferTimeout(timeout)
	if !mb.IsEmpty() {
		atomic.AddInt64(&r.count, int64(mb.Len()))
	}
	return mb, err
}

// Interrupt implements common.Interruptible
func (r *CountingReader) Interrupt() {
	if timeoutReader, ok := r.reader.(interface{ Interrupt() }); ok {
		timeoutReader.Interrupt()
	}
}

// GetCount returns the total bytes read
func (r *CountingReader) GetCount() int64 {
	return atomic.LoadInt64(&r.count)
}

// GetDuration returns the duration since this reader was created
func (r *CountingReader) GetDuration() int64 {
	return int64(time.Since(r.startTime).Seconds())
}

// Close closes the reader
func (r *CountingReader) Close() error {
	if !atomic.CompareAndSwapInt32(&r.closed, 0, 1) {
		return nil // Already closed
	}

	if closer, ok := r.reader.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// CountingWriter wraps a buf.Writer and counts bytes written
type CountingWriter struct {
	writer    buf.Writer
	count     int64
	startTime time.Time
	closed    int32
	mu        sync.Mutex
}

// NewCountingWriter creates a new CountingWriter wrapping the given writer
func NewCountingWriter(writer buf.Writer) *CountingWriter {
	return &CountingWriter{
		writer:    writer,
		startTime: time.Now(),
	}
}

// WriteMultiBuffer implements buf.Writer
func (w *CountingWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	if !mb.IsEmpty() {
		atomic.AddInt64(&w.count, int64(mb.Len()))
	}
	return w.writer.WriteMultiBuffer(mb)
}

// GetCount returns the total bytes written
func (w *CountingWriter) GetCount() int64 {
	return atomic.LoadInt64(&w.count)
}

// GetDuration returns the duration since this writer was created
func (w *CountingWriter) GetDuration() int64 {
	return int64(time.Since(w.startTime).Seconds())
}

// Close implements io.Closer
func (w *CountingWriter) Close() error {
	if !atomic.CompareAndSwapInt32(&w.closed, 0, 1) {
		return nil // Already closed
	}

	return common.Close(w.writer)
}

// Interrupt implements common.Interruptible
func (w *CountingWriter) Interrupt() {
	common.Interrupt(w.writer)
}

// BandwidthEmitter sends bandwidth records to a Unix socket
type BandwidthEmitter struct {
	socketPath string
	mu         sync.Mutex
}

// NewBandwidthEmitter creates a new BandwidthEmitter
func NewBandwidthEmitter(socketPath string) *BandwidthEmitter {
	return &BandwidthEmitter{
		socketPath: socketPath,
	}
}

// Emit sends a bandwidth record to the Unix socket
func (e *BandwidthEmitter) Emit(record *BandwidthRecord) {
	go func() {
		e.mu.Lock()
		defer e.mu.Unlock()

		conn, err := net.DialTimeout("unix", e.socketPath, 5*time.Second)
		if err != nil {
			errors.LogWarning(context.Background(), "Failed to connect to bandwidth socket: ", err)
			return
		}
		defer conn.Close()

		data, err := json.Marshal(record)
		if err != nil {
			errors.LogWarning(context.Background(), "Failed to marshal bandwidth record: ", err)
			return
		}

		data = append(data, '\n')
		_, err = conn.Write(data)
		if err != nil {
			errors.LogWarning(context.Background(), "Failed to write bandwidth record: ", err)
		}
	}()
}

// ConnectionBandwidthData holds the aggregated bandwidth data for a complete connection
type ConnectionBandwidthData struct {
	upTracker   *CountingReader
	downTracker *CountingWriter
	user        string
	domain      string
	inboundTag  string
	outboundTag string
	sourceIP    string
	sourcePort  int
	destPort    int
	network     string
	protocol    string
	startTime   time.Time
}

// NewConnectionBandwidthData creates tracking data for a connection
func NewConnectionBandwidthData(upTracker *CountingReader, downTracker *CountingWriter, user, domain, inboundTag, outboundTag, sourceIP string, sourcePort, destPort int, network, protocol string) *ConnectionBandwidthData {
	return &ConnectionBandwidthData{
		upTracker:   upTracker,
		downTracker: downTracker,
		user:        user,
		domain:      domain,
		inboundTag:  inboundTag,
		outboundTag: outboundTag,
		sourceIP:    sourceIP,
		sourcePort:  sourcePort,
		destPort:    destPort,
		network:     network,
		protocol:    protocol,
		startTime:   time.Now(),
	}
}

// BuildRecord creates the bandwidth record with specified byte counts
func (d *ConnectionBandwidthData) BuildRecord(upBytes, downBytes int64) *BandwidthRecord {
	return &BandwidthRecord{
		User:        d.user,
		Domain:      d.domain,
		InboundTag:  d.inboundTag,
		OutboundTag: d.outboundTag,
		SourceIP:    d.sourceIP,
		SourcePort:  d.sourcePort,
		DestPort:    d.destPort,
		Network:     d.network,
		Protocol:    d.protocol,
		UpBytes:     upBytes,
		DownBytes:   downBytes,
		Duration:    int64(time.Since(d.startTime).Seconds()),
		Timestamp:   time.Now(),
	}
}

// GlobalBandwidthEmitter is the global emitter instance
var GlobalBandwidthEmitter *BandwidthEmitter

func getCountingReader(link *transport.Link) *CountingReader {
	if link == nil || link.Reader == nil {
		return nil
	}

	if reader, ok := link.Reader.(*CountingReader); ok {
		return reader
	}

	if cached, ok := link.Reader.(*cachedReader); ok {
		if reader, ok := cached.reader.(*CountingReader); ok {
			return reader
		}
	}

	return nil
}

func getCountingWriter(link *transport.Link) *CountingWriter {
	if link == nil || link.Writer == nil {
		return nil
	}

	if writer, ok := link.Writer.(*CountingWriter); ok {
		return writer
	}

	return nil
}

// InitBandwidthEmitter initializes the global emitter with the socket path
func InitBandwidthEmitter(socketPath string) {
	if socketPath == "" {
		socketPath = "/tmp/xray_bandwidth.sock"
	}
	GlobalBandwidthEmitter = NewBandwidthEmitter(socketPath)
}

// EmitBandwidth emits a bandwidth record for the given link when the connection ends
func EmitBandwidth(ctx context.Context, inbound *transport.Link, outbound *transport.Link, domain string, dest corenet.Destination) {
	if GlobalBandwidthEmitter == nil {
		InitBandwidthEmitter("")
	}

	// outbound.Reader = data from user (UP)
	// outbound.Writer = data to user (DOWN)
	upTracker := getCountingReader(outbound)
	downTracker := getCountingWriter(outbound)

	// Fallback to inbound if outbound is not wrapped (less ideal)
	if upTracker == nil {
		upTracker = getCountingReader(inbound)
	}
	if downTracker == nil {
		downTracker = getCountingWriter(inbound)
	}

	// Only emit if we have tracking
	if upTracker != nil || downTracker != nil {
		var user, inboundTag, sourceIP string
		var sourcePort int

		if sessionInbound := session.InboundFromContext(ctx); sessionInbound != nil {
			if sessionInbound.User != nil {
				user = sessionInbound.User.Email
			}
			inboundTag = sessionInbound.Tag
			if sessionInbound.Source.IsValid() {
				sourceIP = sessionInbound.Source.Address.String()
				sourcePort = int(sessionInbound.Source.Port)
			}
		}

		if user == "" {
			return
		}

		data := NewConnectionBandwidthData(
			upTracker,
			downTracker,
			user,
			domain,
			inboundTag,
			"",
			sourceIP,
			sourcePort,
			0,
			"",
			"",
		)

		// Initial properties
		if content := session.ContentFromContext(ctx); content != nil {
			data.protocol = content.Protocol
		}
		if outbounds := session.OutboundsFromContext(ctx); len(outbounds) > 0 {
			data.outboundTag = outbounds[len(outbounds)-1].Tag
		}
		if dest.IsValid() {
			data.destPort = int(dest.Port)
			data.network = dest.Network.String()
			if data.domain == "" {
				data.domain = dest.Address.String()
			}
		}

		// Emit IMMEDATELY
		initialUp := int64(0)
		if upTracker != nil {
			initialUp = upTracker.GetCount()
		}
		initialDown := int64(0)
		if downTracker != nil {
			initialDown = downTracker.GetCount()
		}
		GlobalBandwidthEmitter.Emit(data.BuildRecord(initialUp, initialDown))

		// Background polling
		go func() {
			ticker := time.NewTicker(20 * time.Second)
			defer ticker.Stop()

			lastUp := initialUp
			lastDown := initialDown

			for {
				select {
				case <-ticker.C:
					currentUp := int64(0)
					if data.upTracker != nil {
						currentUp = data.upTracker.GetCount()
					}
					currentDown := int64(0)
					if data.downTracker != nil {
						currentDown = data.downTracker.GetCount()
					}

					deltaUp := currentUp - lastUp
					deltaDown := currentDown - lastDown

					if deltaUp > 0 || deltaDown > 0 {
						if content := session.ContentFromContext(ctx); content != nil {
							data.protocol = content.Protocol
						}
						if outbounds := session.OutboundsFromContext(ctx); len(outbounds) > 0 {
							data.outboundTag = outbounds[len(outbounds)-1].Tag
						}
						GlobalBandwidthEmitter.Emit(data.BuildRecord(deltaUp, deltaDown))
						lastUp = currentUp
						lastDown = currentDown
					}
				case <-ctx.Done():
					currentUp := int64(0)
					if data.upTracker != nil {
						currentUp = data.upTracker.GetCount()
					}
					currentDown := int64(0)
					if data.downTracker != nil {
						currentDown = data.downTracker.GetCount()
					}

					deltaUp := currentUp - lastUp
					deltaDown := currentDown - lastDown

					if deltaUp > 0 || deltaDown > 0 {
						if content := session.ContentFromContext(ctx); content != nil {
							data.protocol = content.Protocol
						}
						if outbounds := session.OutboundsFromContext(ctx); len(outbounds) > 0 {
							data.outboundTag = outbounds[len(outbounds)-1].Tag
						}
						GlobalBandwidthEmitter.Emit(data.BuildRecord(deltaUp, deltaDown))
					}
					return
				}
			}
		}()
	}
}
