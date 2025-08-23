#!/usr/bin/env python3
"""
Moonlight WebSocket Client for Python
"""

import asyncio
import json
import logging
import time
from typing import Dict, Any
import websockets

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class MoonlightWebSocketClient:
    def __init__(self, server_url: str, config: Dict[str, Any]):
        self.server_url = server_url
        self.config = config
        self.websocket = None
        self.connected = False
        self.reconnect_interval = 5  # seconds
        self.heartbeat_interval = 30  # seconds
        self.heartbeat_task = None

    async def connect(self):
        """Connect to the WebSocket server"""
        while True:
            try:
                logger.info(f"Connecting to {self.server_url}...")
                self.websocket = await websockets.connect(self.server_url)
                self.connected = True
                logger.info("WebSocket connection established")

                # Register client
                await self.register()

                # Start heartbeat
                self.heartbeat_task = asyncio.create_task(self.heartbeat_loop())

                # Listen for messages
                await self.message_loop()

            except websockets.exceptions.ConnectionClosed:
                logger.info("WebSocket connection closed")
                self.connected = False
                if self.heartbeat_task:
                    self.heartbeat_task.cancel()

            except Exception as e:
                logger.error(f"Connection error: {e}")

            finally:
                self.connected = False
                if self.websocket:
                    await self.websocket.close()

            # Wait before reconnecting
            logger.info(f"Reconnecting in {self.reconnect_interval} seconds...")
            await asyncio.sleep(self.reconnect_interval)

    async def register(self):
        """Register client with the server"""
        register_message = {
            "type": "register",
            "payload": {
                "ip": self.config["ip"],
                "node_id": self.config["node_id"],
                "token": self.config["token"],
                "region": self.config["region"],
                "port": self.config.get("port", 3000)
            }
        }
        await self.send(register_message)

    async def heartbeat_loop(self):
        """Send periodic heartbeat messages"""
        while self.connected:
            try:
                heartbeat_message = {
                    "type": "heartbeat",
                    "payload": {
                        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
                    }
                }
                await self.send(heartbeat_message)
                await asyncio.sleep(self.heartbeat_interval)
            except Exception as e:
                logger.error(f"Heartbeat error: {e}")
                break

    async def message_loop(self):
        """Listen for incoming messages"""
        try:
            async for message in self.websocket:
                try:
                    data = json.loads(message)
                    await self.handle_message(data)
                except json.JSONDecodeError as e:
                    logger.error(f"Failed to parse message: {e}")
        except Exception as e:
            logger.error(f"Message loop error: {e}")

    async def handle_message(self, message: Dict[str, Any]):
        """Handle incoming messages"""
        msg_type = message.get("type")
        
        if msg_type == "registered":
            logger.info("Successfully registered with server")
            
        elif msg_type == "heartbeat_ack":
            logger.info("Heartbeat acknowledged")
            
        elif msg_type == "task":
            await self.handle_task(message)
            
        elif msg_type == "error":
            logger.error(f"Server error: {message.get('payload')}")
            
        else:
            logger.warning(f"Unknown message type: {msg_type}")

    async def handle_task(self, message: Dict[str, Any]):
        """Handle incoming task"""
        task_id = message.get("task_id")
        payload = message.get("payload")
        
        logger.info(f"Received task {task_id}: {payload}")
        
        try:
            # Process the task
            result = await self.process_task(payload)
            
            # Send task response
            response_message = {
                "type": "task_response",
                "payload": {
                    "task_id": task_id,
                    "response": result["data"],
                    "status": result["status"],
                    "headers": result.get("headers", {})
                }
            }
            
            await self.send(response_message)
            logger.info(f"Task {task_id} completed successfully")
            
        except Exception as e:
            logger.error(f"Task {task_id} failed: {e}")
            
            # Send error response
            error_response = {
                "type": "task_response",
                "payload": {
                    "task_id": task_id,
                    "response": json.dumps({"error": str(e)}),
                    "status": 500,
                    "headers": {"Content-Type": "application/json"}
                }
            }
            
            await self.send(error_response)

    async def process_task(self, payload: str) -> Dict[str, Any]:
        """Process a task (implement your actual logic here)"""
        # Parse the payload
        
        # Simulate some processing time
        await asyncio.sleep(0.1)
        
        # For this example, we'll just echo back the payload
        return {
            "data": json.dumps({
                "processed": True,
                "original": payload,
                "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
            }),
            "status": 200,
            "headers": {"Content-Type": "application/json"}
        }

    async def send(self, message: Dict[str, Any]):
        """Send a message to the server"""
        if self.connected and self.websocket:
            await self.websocket.send(json.dumps(message))
        else:
            logger.error("Cannot send message: not connected")

    async def disconnect(self):
        """Disconnect from the server"""
        self.connected = False
        if self.heartbeat_task:
            self.heartbeat_task.cancel()
        if self.websocket:
            await self.websocket.close()


async def main():
    """Main function"""
    config = {
        "ip": "192.168.1.100",
        "node_id": "node-003",
        "token": "supersecrettokenox1",
        "region": "us-west",
        "port": 3000
    }
    
    client = MoonlightWebSocketClient("ws://localhost:8080/ws", config)
    
    try:
        await client.connect()
    except KeyboardInterrupt:
        logger.info("Shutting down...")
        await client.disconnect()


if __name__ == "__main__":
    asyncio.run(main())

