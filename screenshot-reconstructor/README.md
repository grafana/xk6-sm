# Screenshot Reconstructor

This tool reconstructs images from the screenshot server's log output. It takes the base64-encoded chunks from the logs and reassembles them into complete images.

## Installation

```bash
npm install
```

## Usage

1. First, build the TypeScript files:
```bash
npm run build
```

2. Run the CLI tool:
```bash
node dist/cli.js <log-file> [output-dir]
```

Where:
- `<log-file>` is the path to the log file containing the screenshot chunks
- `[output-dir]` is an optional output directory (defaults to `./output`)

## Example

Given a log file containing entries like:
```
time="2024-03-21T10:00:00Z" level=info sha=abc123 count=2 index=1 content=base64chunk1 filename=screenshot1.png
time="2024-03-21T10:00:00Z" level=info sha=abc123 count=2 index=2 content=base64chunk2 filename=screenshot1.png
```

Running:
```bash
node dist/cli.js screenshots.log ./reconstructed
```

Will reconstruct the images and save them to the `./reconstructed` directory.

## How it Works

1. The tool reads the log file and parses each line to extract:
   - SHA hash (to group chunks of the same image)
   - Total chunk count
   - Chunk index
   - Base64-encoded content
   - Original filename

2. It groups chunks by their SHA hash and verifies that all chunks are present

3. The chunks are combined in order and decoded from base64

4. The reconstructed images are saved to the output directory with their original filenames

## Error Handling

- If any chunks are missing for an image, a warning is logged and that image is skipped
- If a log line cannot be parsed, it is skipped with a warning
- The tool will create the output directory if it doesn't exist 