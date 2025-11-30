import asyncio
import logging
import random
from datetime import datetime

import grpc
import controller_pb2, controller_pb2_grpc

from kafka_producer import KafkaTelemetryProducer
from callback_client import CallbackClient

logger = logging.getLogger(__name__)

class LocalControllerService(controller_pb2_grpc.LocalControllerServicer):
    def __init__(self):
        self.state = ""

        # running async tasks
        self.telemetry_task = None
        self.callback_task = None

        # components
        self.kafka: KafkaTelemetryProducer
        self.callback_client: CallbackClient

    async def StartTelemetry(self, request, context):
        broker = request.kafka_broker_addr
        topic = request.topic or "telemetry"

        # cancel previous task if exists
        if self.telemetry_task and not self.telemetry_task.done():
            self.telemetry_task.cancel()

        self.kafka = KafkaTelemetryProducer(broker, topic)
        await self.kafka.start()

        self.telemetry_task = asyncio.create_task(self._telemetry_loop())
        msg = f"Started telemetry to {broker} on topic {topic}"
        logger.info(msg)
        return controller_pb2.StartResponse(status="OK", message=msg)

    async def _telemetry_loop(self):
        while True:
            timestamp = datetime.utcnow().isoformat()
            msg = f"{timestamp} - TELEMETRY"
            await self.kafka.send(msg)
            await asyncio.sleep(5)

    async def StartCallback(self, request, context):
        server_addr = request.server_addr

        if self.callback_task and not self.callback_task.done():
            self.callback_task.cancel()

        self.callback_client = CallbackClient(server_addr)
        await self.callback_client.start()

        self.callback_task = asyncio.create_task(self._callback_loop())
        msg = f"Started callback to {server_addr}"
        logger.info(msg)
        return controller_pb2.StartResponse(status="OK", message=msg)

    async def _callback_loop(self):
        while True:
            await asyncio.sleep(random.randint(2, 10))  # 3-5 mins
            if self.state:
                await self.callback_client.send_state(self.state)

    async def Configure(self, request, context):
        self.state = request.msg
        logger.info(f"State updated to: {self.state}")
        return controller_pb2.ConfigureResponse(status="OK")