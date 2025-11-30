import asyncio
import logging
import aiohttp

logger = logging.getLogger(__name__)

class CallbackClient:
    def __init__(self, server_addr: str):
        """
        server_addr: full URL, e.g. "http://localhost:8080/callback"
        """
        self.server_addr = server_addr
        self.session = None

    async def start(self):
        self.session = aiohttp.ClientSession()

    async def send_state(self, msg: str):
        """
        Sends {"msg": "..."} to the callback HTTP server.
        """
        if not self.session:
            raise RuntimeError("CallbackClient not started. Call start() first.")

        try:
            payload = {"msg": msg}

            async with self.session.post(self.server_addr, json=payload) as resp:
                if resp.status != 200:
                    body = await resp.text()
                    logger.error(
                        f"Callback HTTP error {resp.status} for {self.server_addr}: {body}"
                    )
                else:
                    logger.info(f"Callback: state '{msg}' delivered to {self.server_addr}")

        except Exception as e:
            logger.error(f"Callback HTTP failed: {e}")

    async def close(self):
        if self.session:
            await self.session.close()
