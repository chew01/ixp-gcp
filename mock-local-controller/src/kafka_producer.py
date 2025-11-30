import asyncio
from aiokafka import AIOKafkaProducer
import logging

logger = logging.getLogger(__name__)

class KafkaTelemetryProducer:
    def __init__(self, broker_addr: str, topic: str):
        self.broker_addr = broker_addr
        self.topic = topic
        self.producer: AIOKafkaProducer

    async def start(self):
        self.producer = AIOKafkaProducer(bootstrap_servers=self.broker_addr)
        await self.producer.start()
        logger.info(f"Kafka producer connected to {self.broker_addr}")

    async def stop(self):
        if self.producer:
            await self.producer.stop()

    async def send(self, msg: str):
        try:
            await self.producer.send_and_wait(self.topic, msg.encode())
        except Exception as e:
            logger.error(f"Error sending Kafka message: {e}")