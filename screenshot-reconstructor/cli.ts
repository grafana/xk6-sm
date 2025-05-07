/// <reference types="node" />

import { readFile } from 'node:fs/promises';
import { parse } from 'node:path';
import { reconstructImages, saveImages } from './reconstruct.js';

interface LogLine {
    sha: string;
    count: number;
    index: number;
    content: string;
    filename: string;
}

async function main(): Promise<void> {
    if (process.argv.length < 3) {
        console.error('Usage: ts-node cli.ts <log-file> [output-dir]');
        process.exit(1);
    }

    const logFile = process.argv[2];
    const outputDir = process.argv[3] || './output';

    try {
        // Read and parse the log file
        const logContent = await readFile(logFile, 'utf-8');
        console.log('Raw log content:', logContent);
        
        const logLines = logContent.split('\n')
            .filter((line: string) => line.trim() !== '')
            .map((line: string) => {
                console.log('Processing line:', line);
                try {
                    // Parse the log line which is in the format:
                    // time="2024-03-21T10:00:00Z" level=info sha=abc123 count=2 index=1 content=base64data filename=screenshot.png
                    const entries = line.split(' ')
                        .filter((part: string) => part.includes('='))
                        .map((part: string) => {
                            const [key, value] = part.split('=');
                            console.log('Found key-value pair:', key, value);
                            return [key, value.replace(/^"|"$/g, '')]; // Remove quotes
                        });
                    
                    const logLine: LogLine = {
                        sha: '',
                        count: 0,
                        index: 0,
                        content: '',
                        filename: ''
                    };

                    for (const [key, value] of entries) {
                        console.log('Processing key-value:', key, value);
                        switch (key) {
                            case 'sha':
                                logLine.sha = value;
                                break;
                            case 'count':
                                logLine.count = parseInt(value, 10);
                                break;
                            case 'index':
                                logLine.index = parseInt(value, 10);
                                break;
                            case 'content':
                                logLine.content = value;
                                break;
                            case 'filename':
                                logLine.filename = value;
                                break;
                        }
                    }

                    console.log('Parsed log line:', logLine);
                    return logLine;
                } catch (error) {
                    console.warn(`Failed to parse line: ${line}`);
                    return null;
                }
            })
            .filter((line): line is LogLine => line !== null);

        // Reconstruct and save images
        const images = reconstructImages(logLines);
        await saveImages(images, outputDir);
        
        console.log(`Successfully reconstructed ${images.size} images to ${outputDir}`);
    } catch (error) {
        console.error('Error:', error);
        process.exit(1);
    }
}

main().catch(console.error); 