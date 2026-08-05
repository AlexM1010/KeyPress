import { Position } from '@xyflow/svelte';
import type { NodeShape } from './types';

export const nodeShapes: Record<string, NodeShape> = {
  process: {
    name: 'process',
    svgPath: 'M 0 0 H 100 V 50 H 0 Z',
    defaultSize: { width: 100, height: 50 },
    defaultHandles: [
      { type: 'target', position: Position.Left, offsetY: 50 },
      { type: 'source', position: Position.Right, offsetY: 50 }
    ]
  },
  
  decision: {
    name: 'decision',
    svgPath: 'M 50 0 L 100 50 L 50 100 L 0 50 Z',
    defaultSize: { width: 100, height: 100 },
    defaultHandles: [
      { type: 'source', position: Position.Right, offsetY: 50 },
      { type: 'target', position: Position.Top, offsetY: 50 },
      { type: 'target', position: Position.Bottom, offsetY: 50 }
    ]
  },
  
  start: {
    name: 'start',
    svgPath: 'M 20 0 H 80 A 20 25 0 1 1 80 50 H 20 A 20 25 0 1 1 20 0',
    defaultSize: { width: 100, height: 50 },
    defaultHandles: [
      { type: 'source', position: Position.Right, offsetY: 50 }
    ]
  },

  end: {
    name: 'end',
    svgPath: 'M 20 0 H 80 A 20 25 0 1 1 80 50 H 20 A 20 25 0 1 1 20 0',
    defaultSize: { width: 100, height: 50 },
    defaultHandles: [
      { type: 'source', position: Position.Left, offsetY: 50 }
    ]
  },
  
  input: {
    name: 'input',
    svgPath: 'M 20 0 H 100 L 80 50 H 0 Z',
    defaultSize: { width: 100, height: 50 },
    defaultHandles: [
      { type: 'target', position: Position.Left, offsetX: 10, offsetY: 50 },
      { type: 'source', position: Position.Right, offsetX: 10, offsetY: 50 }
    ],
  },
  
  output: {
    name: 'output',
    svgPath: 'M 0 0 H 80 L 100 100 H 20 Z',
    defaultSize: { width: 100, height: 100 },
    defaultHandles: [
      { type: 'target', position: Position.Left, offsetY: 50 },
      { type: 'source', position: Position.Right, offsetY: 50 }
    ]
  },
  
  store: {
    name: 'store',
    svgPath: `
      M 0 10
      A 50 10 0 0 0 100 10
      A 50 10 0 0 0 0 10
      L 0 90
      A 50 10 0 0 0 100 90
      L 100 10
    `,
    defaultSize: { width: 100, height: 100 },
    defaultHandles: [
      { type: 'target', position: Position.Left, offsetY: 50},
      { type: 'source', position: Position.Right, offsetY: 50},
    ]
  },
  
  delay: {
    name: 'delay',
    svgPath: 'M 0 0 H 70 A 30 25 0 1 1 70 50 H 0 Z',
    defaultSize: { width: 100, height: 50 },
    defaultHandles: [
      { type: 'target', position: Position.Left, offsetY: 50 },
      { type: 'source', position: Position.Right, offsetY: 50 }
    ]
  },

  action: {
    name: "action-node",
    svgPath: 'M10 10 L90 10 L90 40 L10 40 Z', // Replace with actual path
    defaultSize: { width: 100, height: 50 },
    defaultHandles: [
      { type: 'target', position: Position.Left, offsetY: 50 },
      { type: 'source', position: Position.Right, offsetY: 50 }
    ],
  },
};