import asyncio
import logging
import grpc
import controller_pb2_grpc
from controller_server import LocalControllerService

logging.basicConfig(level=logging.INFO)

async def serve():
    server = grpc.aio.server()
    controller_pb2_grpc.add_LocalControllerServicer_to_server(LocalControllerService(), server)

    server.add_insecure_port('[::]:50051')
    await server.start()
    logging.info("Local Controller gRPC server running on :50051")

    await server.wait_for_termination()

if __name__ == '__main__':
    asyncio.run(serve())