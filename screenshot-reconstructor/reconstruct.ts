/// <reference types="node" />
import { createHash } from 'node:crypto';

interface LogEntry {
    sha: string;
    count: number;
    index: number;
    content: string;
    filename: string;
}

/**
 * Calculates SHA256 hash of a buffer
 * @param data Buffer to hash
 * @returns SHA256 hash as hex string
 */
function calculateSHA256(data: Buffer): string {
    return createHash('sha256').update(data).digest('hex');
}

/**
 * Verifies if the first chunk contains a valid PNG header
 * @param firstChunk First chunk of base64 data
 * @returns true if the chunk contains a valid PNG header
 */
function hasValidPNGHeader(firstChunk: string): boolean {
    const decoded = Buffer.from(firstChunk, 'base64');
    // PNG header: 89 50 4E 47 0D 0A 1A 0A
    return decoded.length >= 8 && 
           decoded[0] === 0x89 && 
           decoded[1] === 0x50 && 
           decoded[2] === 0x4E && 
           decoded[3] === 0x47;
}

/**
 * Reconstructs an image from a series of log entries.
 * @param entries Array of log entries containing image chunks
 * @returns Map of SHA to reconstructed image data
 */
export function reconstructImages(entries: LogEntry[]): Map<string, { data: Buffer; filename: string }> {
    console.log(`Processing ${entries.length} log entries`);
    
    // Group entries by SHA
    const chunksBySha = new Map<string, { chunks: LogEntry[]; filename: string }>();
    
    for (const entry of entries) {
        console.log(`Processing entry:`, {
            sha: entry.sha,
            count: entry.count,
            index: entry.index,
            filename: entry.filename
        });
        
        const existing = chunksBySha.get(entry.sha);
        if (existing) {
            existing.chunks.push(entry);
        } else {
            chunksBySha.set(entry.sha, { chunks: [entry], filename: entry.filename });
        }
    }

    console.log(`Found ${chunksBySha.size} unique images to reconstruct`);

    // Reconstruct each image
    const reconstructedImages = new Map<string, { data: Buffer; filename: string }>();
    
    for (const [expectedSha, { chunks, filename }] of chunksBySha) {
        console.log(`\nProcessing image ${filename} (${expectedSha}):`);
        console.log(`- Total chunks found: ${chunks.length}`);
        console.log(`- Expected chunks: ${chunks[0].count}`);
        
        // Sort chunks by index
        chunks.sort((a, b) => a.index - b.index);
        
        // Verify we have all chunks
        if (chunks.length !== chunks[0].count) {
            console.warn(`Missing chunks for ${expectedSha}. Expected ${chunks[0].count}, got ${chunks.length}`);
            continue;
        }

        try {
            // Verify first chunk has PNG header
            const firstChunk = chunks[0].content;
            if (!hasValidPNGHeader(firstChunk)) {
                console.error(`First chunk does not contain valid PNG header for ${filename}`);
                console.log(`First chunk starts with: ${firstChunk.substring(0, 20)}...`);
                continue;
            }

            // Process each chunk
            let allBytes = Buffer.alloc(0);
            for (const chunk of chunks) {
                console.log(`Chunk ${chunk.index}:`);
                console.log(`- Length: ${chunk.content.length}`);
                console.log(`- First 20 chars: ${chunk.content.substring(0, 20)}...`);
                console.log(`- Last 20 chars: ${chunk.content.substring(chunk.content.length - 20)}...`);

                // Decode each chunk individually
                try {
                    const decoded = Buffer.from(chunk.content, 'base64');
                    if (chunk.index === 1) {
                        console.log(`- First 8 bytes (hex): ${decoded.slice(0, 8).toString('hex')}`);
                    }
                    allBytes = Buffer.concat([allBytes, decoded]);
                } catch (error) {
                    console.error(`Failed to decode chunk ${chunk.index}:`, error);
                    continue;
                }
            }

            if (allBytes.length === 0) {
                console.error('No valid chunks decoded');
                continue;
            }
            
            console.log(`\nReconstructing ${filename} (${expectedSha}):`);
            console.log(`- Total chunks: ${chunks.length}`);
            console.log(`- Decoded buffer length: ${allBytes.length}`);
            console.log(`- First 8 bytes (hex): ${allBytes.slice(0, 8).toString('hex')}`);
            console.log(`- Last 8 bytes (hex): ${allBytes.slice(-8).toString('hex')}`);
            
            // Calculate SHA of reconstructed data
            const actualSha = calculateSHA256(allBytes);
            console.log(`- Expected SHA: ${expectedSha}`);
            console.log(`- Actual SHA: ${actualSha}`);
            
            if (actualSha !== expectedSha) {
                console.error(`SHA mismatch for ${filename}!`);
                console.error(`Expected: ${expectedSha}`);
                console.error(`Got: ${actualSha}`);
                continue;
            }
            
            reconstructedImages.set(expectedSha, { data: allBytes, filename });
            console.log(`✓ Successfully reconstructed ${filename}`);
        } catch (error) {
            console.error(`Failed to reconstruct ${filename} (${expectedSha}):`, error);
        }
    }

    return reconstructedImages;
}

/**
 * Saves reconstructed images to disk
 * @param images Map of SHA to reconstructed image data
 * @param outputDir Directory to save images to
 */
export async function saveImages(images: Map<string, { data: Buffer; filename: string }>, outputDir: string): Promise<void> {
    const fs = await import('node:fs/promises');
    const path = await import('node:path');

    // Create output directory if it doesn't exist
    await fs.mkdir(outputDir, { recursive: true });

    // Save each image
    for (const [sha, { data, filename }] of images) {
        // Split filename into name and extension
        const { name, ext } = path.parse(filename);
        // Create new filename with SHA: original-name-sha.extension
        const newFilename = `${name}-${sha}${ext}`;
        const outputPath = path.join(outputDir, newFilename);

        await fs.writeFile(outputPath, data);
        console.log(`Saved ${newFilename} to ${outputPath}`);
    }
}

/**
 * Example usage:
 * 
 * const logEntries = [
 *     {
 *         sha: "abc123",
 *         count: 2,
 *         index: 1,
 *         content: "base64chunk1",
 *         filename: "screenshot1.png"
 *     },
 *     {
 *         sha: "abc123",
 *         count: 2,
 *         index: 2,
 *         content: "base64chunk2",
 *         filename: "screenshot1.png"
 *     }
 * ];
 * 
 * const images = reconstructImages(logEntries);
 * await saveImages(images, "./output");
 */ 