/// <reference types="node" />

import * as fs from 'fs';
import * as crypto from 'crypto';

interface LogEntry {
  sha: string;
  count: number;
  index: number;
  content: string;
  filename: string;
}

function validateBase64(base64: string): boolean {
  // Check if string is valid base64
  const base64Regex = /^[A-Za-z0-9+/]*={0,2}$/;
  if (!base64Regex.test(base64)) {
    console.error('Invalid base64 string - contains invalid characters');
    return false;
  }

  // Check if length is valid (should be multiple of 4)
  if (base64.length % 4 !== 0) {
    console.error(`Invalid base64 length: ${base64.length} (should be multiple of 4)`);
    return false;
  }

  return true;
}

function padBase64(base64: string): string {
  const padding = base64.length % 4;
  if (padding === 0) return base64;
  return base64 + '='.repeat(4 - padding);
}

function reconstructImage(chunks: string[], expectedSha: string, filename: string): boolean {
  console.log(`\nReconstructing ${filename} (${expectedSha}):`);
  console.log(`- Total chunks: ${chunks.length}`);

  // Combine chunks
  const combinedBase64 = chunks.join('');
  console.log(`- Combined base64 length: ${combinedBase64.length}`);

  // Validate base64
  if (!validateBase64(combinedBase64)) {
    console.error('Invalid base64 data after combining chunks');
    return false;
  }

  // Pad base64 if needed
  const paddedBase64 = padBase64(combinedBase64);
  console.log(`- Padded base64 length: ${paddedBase64.length}`);

  try {
    // Decode base64
    const buffer = Buffer.from(paddedBase64, 'base64');
    console.log(`- Decoded buffer length: ${buffer.length}`);
    console.log(`- First 8 bytes (hex): ${buffer.slice(0, 8).toString('hex')}`);

    // Calculate SHA
    const actualSha = crypto.createHash('sha256').update(buffer).digest('hex');
    console.log(`- Expected SHA: ${expectedSha}`);
    console.log(`- Actual SHA: ${actualSha}`);

    if (actualSha !== expectedSha) {
      console.error(`SHA mismatch for ${filename}!`);
      console.error(`Expected: ${expectedSha}`);
      console.error(`Got: ${actualSha}`);
      return false;
    }

    // Write file
    fs.writeFileSync(filename, buffer);
    console.log(`Successfully reconstructed ${filename}`);
    return true;
  } catch (error) {
    console.error(`Error reconstructing ${filename}:`, error);
    return false;
  }
}

function processLogLine(line: string): LogEntry | null {
  try {
    const match = line.match(/sha=([^,]+),count=(\d+),index=(\d+),content=([^,]+),filename=([^,]+)/);
    if (!match) {
      console.error('Invalid log line format');
      return null;
    }

    const [, sha, countStr, indexStr, content, filename] = match;
    const count = parseInt(countStr, 10);
    const index = parseInt(indexStr, 10);

    // Validate base64 content
    if (content && !validateBase64(content)) {
      console.error('Invalid base64 content in log line');
      return null;
    }

    return {
      sha,
      count,
      index,
      content,
      filename
    };
  } catch (error) {
    console.error('Error processing log line:', error);
    return null;
  }
} 